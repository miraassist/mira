package zigbee

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/miraassist/mira/internal/devices"
	"github.com/miraassist/mira/internal/tools"
)

// ZHA, Home Assistant's own Zigbee (docs/spec/device-onboarding.md, ZHA,
// decisions 11 to 14). Pairing opens with Home Assistant's websocket command
// zha/devices/permit, a subscription: while it lasts, ZHA's gateway messages
// come over it (homeassistant/components/zha/websocket_api.py and
// helpers.py, zha's application/gateway.py): "device_joined" (ieee,
// pairing_status PAIRED), "raw_device_initialized" (manufacturer, model) and
// "device_fully_initialized" with the device's info, its Home Assistant
// device id (device_reg_id) among it. ZHA says nothing when pairing ends: the
// time Mira asked for is the only end (a time, as no signal exists). A
// pairing is closed by permitting for 0 seconds.

// zhaMessage is one of ZHA's gateway messages.
type zhaMessage struct {
	Type       string `json:"type"`
	DeviceInfo struct {
		IEEE          string `json:"ieee"`
		Manufacturer  string `json:"manufacturer"`
		Model         string `json:"model"`
		Name          string `json:"name"`
		UserGivenName string `json:"user_given_name"`
		DeviceRegID   string `json:"device_reg_id"`
		AreaID        string `json:"area_id"`
		PairingStatus string `json:"pairing_status"`
	} `json:"device_info"`
}

// zhaRun is one thread's pairing on ZHA.
type zhaRun struct {
	thread string
	stop   context.CancelFunc
	in     chan json.RawMessage
	cancel func()
	// known are the devices Home Assistant had when pairing opened: one of
	// them initialised again is a known device paired again (decision 14,
	// an assumption until the real run).
	known map[string]bool

	mu       sync.Mutex
	open     bool
	joined   map[string]bool
	told     bool
	deviceID string
}

// zhaLoaded reports whether Home Assistant runs ZHA.
func (p *Pairing) zhaLoaded(ctx context.Context) (bool, error) {
	raw, err := p.o.HA.Call(ctx, map[string]any{"type": "config_entries/get", "domain": "zha"})
	if err != nil {
		return false, err
	}
	var entries []struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		return false, err
	}
	for _, e := range entries {
		if e.State == "loaded" {
			return true, nil
		}
	}
	return false, nil
}

func (p *Pairing) startZHA(ctx context.Context, inv tools.Invocation) (string, error) {
	t := p.t
	thread, err := p.o.Start(ctx, inv, t.plan)
	if err != nil {
		return "", err
	}
	p.endRun(context.WithoutCancel(ctx), thread)
	runCtx, stop := context.WithCancel(context.WithoutCancel(ctx))
	r := &zhaRun{thread: thread, stop: stop, in: make(chan json.RawMessage, 256), known: map[string]bool{}, joined: map[string]bool{}}
	for id := range p.o.Devices() {
		r.known[id] = true
	}
	cancel, err := p.o.HA.SubscribeRaw(runCtx, map[string]any{"type": "zha/devices/permit", "duration": p.o.Seconds},
		func(raw json.RawMessage) {
			select { // on the connection's read loop: only pass it on
			case r.in <- raw:
			default:
				p.log.Warn("a ZHA message dropped, too many at once")
			}
		})
	if err != nil {
		stop()
		p.log.Error("zigbee pairing failed on ZHA", "error", err)
		return fmt.Sprintf(t.pairingFailed, err), nil
	}
	r.cancel = cancel
	r.open = true
	p.mu.Lock()
	p.zruns[thread] = r
	p.mu.Unlock()
	p.log.Info("zigbee pairing open", "thread", thread, "seconds", p.o.Seconds, "network", "zha")
	go func() {
		defer cancel()
		p.watchZHA(runCtx, r)
	}()
	return t.pairingOpen, nil
}

// watchZHA follows a pairing on ZHA until a device is there, the time is
// over, or the thread ended.
func (p *Pairing) watchZHA(ctx context.Context, r *zhaRun) {
	over := time.NewTimer(time.Duration(p.o.Seconds) * time.Second)
	defer over.Stop()
	var safety <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-over.C:
			r.mu.Lock()
			r.open = false
			waiting := len(r.joined) > 0
			r.mu.Unlock()
			if waiting {
				safety = time.After(p.o.Interview)
				continue
			}
			p.log.Info("zigbee pairing ended without a device", "thread", r.thread, "network", "zha")
			p.sayTo(ctx, r.thread, p.t.nothingJoined)
			return
		case <-safety:
			p.log.Warn("zigbee device did not finish its interview in time", "thread", r.thread, "limit", p.o.Interview, "network", "zha")
			p.sayTo(ctx, r.thread, p.t.interviewFailed)
			return
		case raw := <-r.in:
			var m zhaMessage
			if json.Unmarshal(raw, &m) != nil {
				continue
			}
			if p.zhaMessage(ctx, r, m) {
				return
			}
		}
	}
}

// zhaMessage handles one of ZHA's messages; true when the run is over.
func (p *Pairing) zhaMessage(ctx context.Context, r *zhaRun, m zhaMessage) bool {
	info := m.DeviceInfo
	switch m.Type {
	case "device_joined":
		r.mu.Lock()
		r.joined[info.IEEE] = true
		tell := !r.told
		r.told = true
		r.mu.Unlock()
		p.log.Info("zigbee device joined", "thread", r.thread, "ieee", info.IEEE, "network", "zha")
		if tell {
			p.sayTo(ctx, r.thread, p.t.joining)
		}
	case "device_fully_initialized":
		r.mu.Lock()
		joined := r.joined[info.IEEE]
		r.mu.Unlock()
		if !joined || info.DeviceRegID == "" {
			return false
		}
		p.closeZHA(ctx, r)
		if r.known[info.DeviceRegID] {
			name := info.UserGivenName
			if name == "" {
				name = info.Name
			}
			area := p.o.AreaName(p.o.DeviceArea()[info.DeviceRegID])
			p.log.Info("zigbee device known already", "thread", r.thread, "ieee", info.IEEE, "device", info.DeviceRegID, "network", "zha")
			p.sayTo(ctx, r.thread, fmt.Sprintf(p.t.known, name, area))
			p.o.End(ctx, r.thread)
			return true
		}
		r.mu.Lock()
		r.deviceID = info.DeviceRegID
		r.mu.Unlock()
		p.log.Info("zigbee device found", "thread", r.thread, "ieee", info.IEEE, "device", info.DeviceRegID,
			"vendor", info.Manufacturer, "model", info.Model, "network", "zha")
		// ZHA has no name of its own: Home Assistant's name, and the entity
		// ids follow (decision 12).
		p.o.Core.Hold(r.thread, devices.Device{ID: info.DeviceRegID, RenameEntities: true})
		facts := fmt.Sprintf(p.t.zhaFacts, info.Manufacturer, info.Model)
		if err := p.o.Note(ctx, r.thread, facts); err != nil {
			p.log.Warn("could not note the device in its thread", "thread", r.thread, "error", err)
		}
		p.tick(ctx, r.thread, 0)
		question, err := p.o.Question(ctx, facts)
		if err != nil || strings.TrimSpace(question) == "" {
			p.log.Warn("could not word the question for the device; asking with vendor and model", "error", err)
			question = fmt.Sprintf(p.t.found, info.Manufacturer, info.Model)
		}
		p.sayTo(ctx, r.thread, question)
		return true
	}
	return false
}

// closeZHA ends pairing on ZHA if this run opened it: a permit for 0
// seconds, its subscription ended right after its answer.
func (p *Pairing) closeZHA(ctx context.Context, r *zhaRun) {
	r.mu.Lock()
	open := r.open
	r.open = false
	r.mu.Unlock()
	if !open {
		return
	}
	cancel, err := p.o.HA.SubscribeRaw(ctx, map[string]any{"type": "zha/devices/permit", "duration": 0}, func(json.RawMessage) {})
	if err != nil {
		p.log.Warn("could not close zigbee pairing on ZHA; it ends by itself", "seconds", p.o.Seconds, "error", err)
		return
	}
	cancel()
}

func (p *Pairing) sayTo(ctx context.Context, thread, text string) {
	if err := p.o.Ask(ctx, thread, text); err != nil {
		p.log.Error("could not say the zigbee thread's announcement", "thread", thread, "error", err)
	}
}

func (p *Pairing) endZHA(ctx context.Context, thread string) {
	p.mu.Lock()
	r := p.zruns[thread]
	delete(p.zruns, thread)
	p.mu.Unlock()
	if r == nil {
		return
	}
	r.stop()
	p.closeZHA(ctx, r)
	if r.cancel != nil {
		r.cancel()
	}
}
