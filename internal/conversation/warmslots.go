package conversation

import (
	"context"
	"time"

	"github.com/miraassist/mira/internal/agent"
	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/llm"
	"github.com/miraassist/mira/internal/prompt"
)

// A local model with warm_slots keeps the conversation prompt warm in one
// slot while a changed prompt is read into the other (docs/spec/warm-slot.md).
// Turns go on with the prompt the warm slot holds until the new one is read.

// warmSlots is the slot keeper of a model with warm_slots, nil for any
// other; it reads a waiting prompt through this engine.
func (e *Engine) warmSlots(model string) *agent.WarmSlots {
	w := e.agents.Warm(model)
	if w != nil {
		w.OnRead(func() { e.readWarm(model) })
	}
	return w
}

// servedPrefix is the prompt a turn of a channel is built on: the one the
// warm slot holds; a fresher one waits to be read.
func (e *Engine) servedPrefix(model string, w *agent.WarmSlots, fresh prompt.Prefix, ch config.Channel) prompt.Prefix {
	key := string(ch) + "/" + string(fresh.Lang)
	e.servedMu.Lock()
	defer e.servedMu.Unlock()
	if e.served == nil {
		e.served = map[string]map[string]prompt.Prefix{}
	}
	held, ok := e.served[model][key]
	if ok && held.Hash() != fresh.Hash() {
		w.Changed()
		return held
	}
	if e.served[model] == nil {
		e.served[model] = map[string]prompt.Prefix{}
	}
	e.served[model][key] = fresh
	return fresh
}

// readWarm reads the voice channel's prompt into the spare slot, if one
// waits and the model is free; once read, the spare slot is the warm one.
// The other channels share all of it but their short end (decision 9).
func (e *Engine) readWarm(model string) {
	w := e.agents.Warm(model)
	if w == nil {
		return
	}
	ctx, spare, ok := w.BeginReading(context.Background())
	if !ok {
		return
	}
	read := false
	defer func() { w.EndReading(ctx, read) }()
	prefix, err := e.Prefix(ctx, config.ChannelVoice)
	if err != nil {
		e.log.Warn("could not build the prompt to read", "model", model, "error", err)
		return
	}
	built := prompt.WarmupRequest(prefix)
	req := e.chatRequestOn(model, config.ChannelVoice, built.Messages, built)
	req.IDSlot = llm.IntPtr(spare)
	client, m, _ := e.modelFor(model)
	release, err := e.agents.Acquire(ctx, model, m)
	if err != nil {
		return
	}
	started := time.Now()
	err = client.Warmup(ctx, req)
	release()
	switch {
	case ctx.Err() != nil:
		e.log.Info("reading of the prompt stopped for a turn", "model", model, "slot", spare)
		return
	case err != nil:
		e.log.Warn("could not read the prompt", "model", model, "slot", spare, "error", err)
		return
	}
	e.servedMu.Lock()
	if e.served == nil {
		e.served = map[string]map[string]prompt.Prefix{}
	}
	e.served[model] = map[string]prompt.Prefix{string(config.ChannelVoice) + "/" + string(prefix.Lang): prefix}
	e.servedMu.Unlock()
	read = true
	e.log.Info("slot warmed", "model", model, "slot", spare, "prefix_hash", prefix.Hash(), "took", time.Since(started).Round(time.Millisecond))
}
