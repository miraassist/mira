package wifi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/miraassist/mira/internal/excerpt"
)

// AP is a Wi-Fi network a scan saw.
type AP struct {
	SSID      string
	Frequency int // MHz
	Open      bool
	Strength  int // percent
}

// Radio is the Wi-Fi of the machine Mira runs on.
type Radio interface {
	// Free is "" when the radio can be used (docs/spec/wifi-onboarding.md,
	// decision 6): a Wi-Fi interface present and switched on, and not
	// connected or connected while Home Assistant is reached over another
	// interface; else why not.
	Free(ctx context.Context) string
	Scan(ctx context.Context) ([]AP, error)
	// Join joins a network so that it never becomes the default route and
	// returns once it is up; Leave leaves it and forgets it.
	Join(ctx context.Context, ssid, password string) (any, error)
	Leave(ctx context.Context, joined any) error
	// Tidy forgets what a run before left behind (Mira's profiles).
	Tidy(ctx context.Context) error
	Kind() string
}

// Detect finds how Mira reaches the Wi-Fi (docs/spec/wifi-onboarding.md, How
// Mira reaches the Wi-Fi): the Supervisor on Home Assistant OS, else
// NetworkManager over the system bus; nil and why when neither. haURL is
// Home Assistant's address: a connected Wi-Fi is usable only when the way to
// it runs over another interface.
func Detect(haURL string) (Radio, string) {
	if token := os.Getenv("SUPERVISOR_TOKEN"); token != "" {
		return &Supervisor{Base: "http://supervisor", Token: token, Client: &http.Client{Timeout: 30 * time.Second}}, ""
	}
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, "no system bus (for a container: mount /run/dbus/system_bus_socket): " + err.Error()
	}
	nm := &NetworkManager{conn: conn, haURL: haURL}
	if _, err := nm.wifiDevice(); err != nil {
		conn.Close()
		return nil, "no NetworkManager with a Wi-Fi interface: " + err.Error()
	}
	return nm, ""
}

// profilePrefix names the profiles Mira makes, so a run's leftovers are
// found and forgotten.
const profilePrefix = "mira-onboarding-"

const (
	nmDest      = "org.freedesktop.NetworkManager"
	nmPath      = "/org/freedesktop/NetworkManager"
	nmDevice    = nmDest + ".Device"
	nmWireless  = nmDest + ".Device.Wireless"
	nmAP        = nmDest + ".AccessPoint"
	nmActive    = nmDest + ".Connection.Active"
	nmSettings  = nmDest + ".Settings"
	nmConnSet   = nmDest + ".Settings.Connection"
	deviceWifi  = 2
	stateDiscon = 30
	stateActive = 100
	activeUp    = 2
	activeDown  = 4
)

// NetworkManager is the radio over NetworkManager's D-Bus API (checked
// read-only on a desktop 2026-11-10: DeviceType 2, State 100 when connected,
// LastScan, the access points' Ssid, Frequency, WpaFlags, RsnFlags).
type NetworkManager struct {
	conn  *dbus.Conn
	haURL string
}

func (n *NetworkManager) Kind() string { return "NetworkManager" }

func (n *NetworkManager) prop(path dbus.ObjectPath, iface, name string) (dbus.Variant, error) {
	return n.conn.Object(nmDest, path).GetProperty(iface + "." + name)
}

func (n *NetworkManager) wifiDevice() (dbus.ObjectPath, error) {
	var devices []dbus.ObjectPath
	if err := n.conn.Object(nmDest, nmPath).Call(nmDest+".GetDevices", 0).Store(&devices); err != nil {
		return "", err
	}
	for _, d := range devices {
		v, err := n.prop(d, nmDevice, "DeviceType")
		if err == nil {
			if t, ok := v.Value().(uint32); ok && t == deviceWifi {
				return d, nil
			}
		}
	}
	return "", errors.New("no Wi-Fi interface")
}

func (n *NetworkManager) Free(ctx context.Context) string {
	d, err := n.wifiDevice()
	if err != nil {
		return err.Error()
	}
	v, err := n.prop(d, nmDevice, "State")
	if err != nil {
		return err.Error()
	}
	state, _ := v.Value().(uint32)
	if state < stateDiscon || state > stateActive {
		return fmt.Sprintf("the machine's Wi-Fi is not available (state %d: switched off or without a driver)", state)
	}
	if state == stateDiscon {
		return ""
	}
	name, err := n.prop(d, nmDevice, "Interface")
	if err != nil {
		return err.Error()
	}
	iface, _ := name.Value().(string)
	via, err := RouteTo(ctx, n.haURL)
	return connectedUsable(iface, via, err)
}

// connectedUsable decides for a connected Wi-Fi interface wifi: usable ("")
// when the way to Home Assistant (via) runs over another interface.
func connectedUsable(wifi, via string, err error) string {
	switch {
	case err != nil:
		return "the machine's Wi-Fi is connected, and the way to Home Assistant is not known: " + err.Error()
	case via == wifi:
		return "the machine's Wi-Fi is connected and Home Assistant is reached over it: leaving it would cut Home Assistant off"
	}
	return ""
}

// RouteTo is the interface the machine would use to reach the host of
// rawURL, read from the routing table: a UDP socket is connected to the
// address (no packet is sent) and its own address names the interface.
func RouteTo(ctx context.Context, rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return "", fmt.Errorf("Home Assistant's address %q is not understood", rawURL)
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, u.Hostname())
	if err != nil || len(ips) == 0 {
		return "", fmt.Errorf("Home Assistant's host %q is not resolved: %v", u.Hostname(), err)
	}
	c, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: ips[0].IP, Port: 9})
	if err != nil {
		return "", fmt.Errorf("no route to Home Assistant: %w", err)
	}
	local := c.LocalAddr().(*net.UDPAddr).IP
	c.Close()
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, i := range ifaces {
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.Equal(local) {
				return i.Name, nil
			}
		}
	}
	return "", fmt.Errorf("no interface has the address %s", local)
}

func (n *NetworkManager) Scan(ctx context.Context) ([]AP, error) {
	d, err := n.wifiDevice()
	if err != nil {
		return nil, err
	}
	before, _ := n.prop(d, nmWireless, "LastScan")
	// The signal: LastScan changes when the scan is done.
	if err := n.conn.AddMatchSignal(dbus.WithMatchObjectPath(d), dbus.WithMatchInterface("org.freedesktop.DBus.Properties"),
		dbus.WithMatchMember("PropertiesChanged")); err != nil {
		return nil, err
	}
	defer func() {
		_ = n.conn.RemoveMatchSignal(dbus.WithMatchObjectPath(d), dbus.WithMatchInterface("org.freedesktop.DBus.Properties"),
			dbus.WithMatchMember("PropertiesChanged"))
	}()
	signals := make(chan *dbus.Signal, 16)
	n.conn.Signal(signals)
	defer n.conn.RemoveSignal(signals)
	if err := n.conn.Object(nmDest, d).CallWithContext(ctx, nmWireless+".RequestScan", 0, map[string]dbus.Variant{}).Err; err != nil {
		return nil, fmt.Errorf("the scan was refused: %w", err)
	}
	safety := time.NewTimer(15 * time.Second)
	defer safety.Stop()
wait:
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-safety.C:
			return nil, errors.New("the scan did not finish within 15 s")
		case <-signals:
			if now, err := n.prop(d, nmWireless, "LastScan"); err == nil && now.String() != before.String() {
				break wait
			}
		}
	}
	var paths []dbus.ObjectPath
	if err := n.conn.Object(nmDest, d).CallWithContext(ctx, nmWireless+".GetAllAccessPoints", 0).Store(&paths); err != nil {
		return nil, err
	}
	var aps []AP
	for _, p := range paths {
		var ap AP
		if v, err := n.prop(p, nmAP, "Ssid"); err == nil {
			b, _ := v.Value().([]byte)
			ap.SSID = string(b)
		}
		if v, err := n.prop(p, nmAP, "Frequency"); err == nil {
			f, _ := v.Value().(uint32)
			ap.Frequency = int(f)
		}
		wpa, rsn := uint32(0), uint32(0)
		if v, err := n.prop(p, nmAP, "WpaFlags"); err == nil {
			wpa, _ = v.Value().(uint32)
		}
		if v, err := n.prop(p, nmAP, "RsnFlags"); err == nil {
			rsn, _ = v.Value().(uint32)
		}
		ap.Open = wpa == 0 && rsn == 0
		if v, err := n.prop(p, nmAP, "Strength"); err == nil {
			s, _ := v.Value().(byte)
			ap.Strength = int(s)
		}
		if ap.SSID != "" {
			aps = append(aps, ap)
		}
	}
	return aps, nil
}

// nmProfile is the profile Mira joins a device's Wi-Fi with: never the
// default route (Home Assistant, the model and the home network stay on the
// cable), never connected by itself, named so a run's leftovers are found.
func nmProfile(ssid, password string) map[string]map[string]dbus.Variant {
	settings := map[string]map[string]dbus.Variant{
		"connection": {"id": dbus.MakeVariant(profilePrefix + ssid), "type": dbus.MakeVariant("802-11-wireless"),
			"autoconnect": dbus.MakeVariant(false)},
		"802-11-wireless": {"ssid": dbus.MakeVariant([]byte(ssid)), "mode": dbus.MakeVariant("infrastructure")},
		"ipv4":            {"method": dbus.MakeVariant("auto"), "never-default": dbus.MakeVariant(true)},
		"ipv6":            {"method": dbus.MakeVariant("ignore")},
	}
	if password != "" {
		settings["802-11-wireless-security"] = map[string]dbus.Variant{"key-mgmt": dbus.MakeVariant("wpa-psk"), "psk": dbus.MakeVariant(password)}
	}
	return settings
}

type nmJoined struct {
	settings, active dbus.ObjectPath
}

func (n *NetworkManager) Join(ctx context.Context, ssid, password string) (any, error) {
	d, err := n.wifiDevice()
	if err != nil {
		return nil, err
	}
	settings := nmProfile(ssid, password)
	var j nmJoined
	if err := n.conn.Object(nmDest, nmPath).CallWithContext(ctx, nmDest+".AddAndActivateConnection", 0, settings, d, dbus.ObjectPath("/")).
		Store(&j.settings, &j.active); err != nil {
		return nil, fmt.Errorf("NetworkManager refused to join: %w", err)
	}
	// The signal: the active connection's state becomes activated
	// (StateChanged; read once first, it may be up already).
	state := func() (bool, error) {
		v, err := n.prop(j.active, nmActive, "State")
		if err != nil {
			return false, nil
		}
		switch st, _ := v.Value().(uint32); st {
		case activeUp:
			return true, nil
		case activeDown:
			return false, errors.New("the device's Wi-Fi could not be joined")
		}
		return false, nil
	}
	if err := n.waitSignal(ctx, j.active, 20*time.Second, state); err != nil {
		_ = n.Leave(context.WithoutCancel(ctx), j)
		return nil, err
	}
	return j, nil
}

// waitSignal waits until done says so, looking again at each signal of the
// object at path, up to limit (a safety net).
func (n *NetworkManager) waitSignal(ctx context.Context, path dbus.ObjectPath, limit time.Duration, done func() (bool, error)) error {
	opts := []dbus.MatchOption{dbus.WithMatchObjectPath(path)}
	if err := n.conn.AddMatchSignal(opts...); err != nil {
		return err
	}
	defer func() { _ = n.conn.RemoveMatchSignal(opts...) }()
	signals := make(chan *dbus.Signal, 16)
	n.conn.Signal(signals)
	defer n.conn.RemoveSignal(signals)
	safety := time.NewTimer(limit)
	defer safety.Stop()
	for {
		ok, err := done()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-safety.C:
			return fmt.Errorf("the device's Wi-Fi was not up within %s", limit)
		case <-signals:
		}
	}
}

func (n *NetworkManager) Leave(ctx context.Context, joined any) error {
	j, ok := joined.(nmJoined)
	if !ok {
		return nil
	}
	var errs []error
	if err := n.conn.Object(nmDest, nmPath).CallWithContext(ctx, nmDest+".DeactivateConnection", 0, j.active).Err; err != nil {
		errs = append(errs, err)
	}
	if err := n.conn.Object(nmDest, j.settings).CallWithContext(ctx, nmConnSet+".Delete", 0).Err; err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (n *NetworkManager) Tidy(ctx context.Context) error {
	var conns []dbus.ObjectPath
	if err := n.conn.Object(nmDest, nmPath+"/Settings").CallWithContext(ctx, nmSettings+".ListConnections", 0).Store(&conns); err != nil {
		return err
	}
	var errs []error
	for _, c := range conns {
		var s map[string]map[string]dbus.Variant
		if err := n.conn.Object(nmDest, c).CallWithContext(ctx, nmConnSet+".GetSettings", 0).Store(&s); err != nil {
			continue
		}
		if id, _ := s["connection"]["id"].Value().(string); strings.HasPrefix(id, profilePrefix) {
			if err := n.conn.Object(nmDest, c).CallWithContext(ctx, nmConnSet+".Delete", 0).Err; err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// Supervisor is the radio over the Supervisor's network API on Home
// Assistant OS (the HA app with hassio_api; not tried on a real one yet:
// docs/spec/wifi-onboarding.md, Testing). It has no signal for "connected":
// Mira reads the interface's state once a second until it is (a poll, as
// nothing reports it).
type Supervisor struct {
	Base, Token string
	Client      *http.Client
	// Log gets the warnings; slog's default without one.
	Log *slog.Logger
}

func (s *Supervisor) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default().With("component", "wifi")
}

func (s *Supervisor) Kind() string { return "Supervisor" }

func (s *Supervisor) call(ctx context.Context, method, path string, body any, out any) error {
	var r io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		r = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.Base+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	raw, err := answer(resp)
	if err != nil {
		return fmt.Errorf("the Supervisor: %w", err)
	}
	if out == nil {
		return nil
	}
	var env struct {
		Result string          `json:"result"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("%q not understood: %w", excerpt.Bytes(raw), err)
	}
	return json.Unmarshal(env.Data, out)
}

type svInterface struct {
	Interface string `json:"interface"`
	Type      string `json:"type"`
	Enabled   bool   `json:"enabled"`
	Connected bool   `json:"connected"`
	Primary   bool   `json:"primary"`
}

func (s *Supervisor) wireless(ctx context.Context) (svInterface, error) {
	var info struct {
		Interfaces []svInterface `json:"interfaces"`
	}
	if err := s.call(ctx, http.MethodGet, "/network/info", nil, &info); err != nil {
		return svInterface{}, err
	}
	for _, i := range info.Interfaces {
		if i.Type == "wireless" {
			return i, nil
		}
	}
	return svInterface{}, errors.New("no Wi-Fi interface")
}

func (s *Supervisor) Free(ctx context.Context) string {
	i, err := s.wireless(ctx)
	if err != nil {
		return err.Error()
	}
	// Home Assistant OS: Home Assistant is reached over the Supervisor's own
	// network, never over the Wi-Fi; the primary interface decides
	// (decision 6).
	if i.Connected && i.Primary {
		return "the machine's Wi-Fi is its primary connection: leaving it would cut the machine off"
	}
	return ""
}

func (s *Supervisor) Scan(ctx context.Context) ([]AP, error) {
	i, err := s.wireless(ctx)
	if err != nil {
		return nil, err
	}
	var out struct {
		AccessPoints []struct {
			SSID      string `json:"ssid"`
			Frequency int    `json:"frequency"`
			Signal    int    `json:"signal"`
		} `json:"accesspoints"`
	}
	if err := s.call(ctx, http.MethodGet, "/network/interface/"+i.Interface+"/accesspoints", nil, &out); err != nil {
		return nil, err
	}
	var aps []AP
	for _, a := range out.AccessPoints {
		// The Supervisor does not say whether a network is open: the
		// families' setup networks are taken as they are known.
		aps = append(aps, AP{SSID: a.SSID, Frequency: a.Frequency, Strength: a.Signal, Open: true})
	}
	return aps, nil
}

type svJoined struct{ iface string }

func (s *Supervisor) Join(ctx context.Context, ssid, password string) (any, error) {
	i, err := s.wireless(ctx)
	if err != nil {
		return nil, err
	}
	wifi := map[string]any{"mode": "infrastructure", "ssid": ssid, "auth": "open"}
	if password != "" {
		wifi["auth"], wifi["psk"] = "wpa-psk", password
	}
	if err := s.call(ctx, http.MethodPost, "/network/interface/"+i.Interface+"/update",
		map[string]any{"enabled": true, "ipv4": map[string]any{"method": "auto"}, "wifi": wifi}, nil); err != nil {
		return nil, fmt.Errorf("the Supervisor refused to join: %w", err)
	}
	j := svJoined{iface: i.Interface}
	deadline := time.Now().Add(20 * time.Second)
	warned := false
	for time.Now().Before(deadline) {
		now, err := s.wireless(ctx)
		if err == nil && now.Connected {
			return j, nil
		}
		if err != nil && !warned {
			// Asked again each second within the 20 s (docs/spec/retry.md, row 12).
			s.log().Warn("the Supervisor did not answer while joining the device's Wi-Fi; asking again", "ssid", ssid, "error", err)
			warned = true
		}
		select {
		case <-ctx.Done():
			_ = s.Leave(context.WithoutCancel(ctx), j)
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	_ = s.Leave(ctx, j)
	return nil, errors.New("the device's Wi-Fi was not up within 20 s")
}

func (s *Supervisor) Leave(ctx context.Context, joined any) error {
	j, ok := joined.(svJoined)
	if !ok {
		return nil
	}
	return s.call(ctx, http.MethodPost, "/network/interface/"+j.iface+"/update", map[string]any{"enabled": false}, nil)
}

// Tidy: the Supervisor keeps no profile of Mira's; the interface is left
// switched off by Leave.
func (s *Supervisor) Tidy(context.Context) error { return nil }
