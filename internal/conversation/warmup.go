package conversation

import (
	"context"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/prompt"
)

// RequestWarmup schedules a warmup of every slot. Calls while one is pending
// collapse into it; the reason of the first one is logged.
func (e *Engine) RequestWarmup(reason string) {
	select {
	case e.warm <- reason:
	default:
	}
}

// RunWarmups serves RequestWarmup until ctx ends.
func (e *Engine) RunWarmups(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case reason := <-e.warm:
			e.tel.RecordPrefixRebuild(ctx, reason)
			e.WarmupAll(ctx, reason)
		}
	}
}

// WarmupAll sends one warmup request per channel slot so the first real turn
// finds layers 1 to 5 in the KV cache. Each channel is warmed under its own
// lock so a warmup never interleaves with a turn on the same slot.
//
// Without pinned slots the server picks a slot by common prefix, and all
// channels share everything but the short style message. One warmup of the
// voice channel then covers the expensive part for all of them; three would
// only occupy the server (LM Studio: about 48 seconds each for a cold
// prompt of 9000 tokens) and evict each other's cache entries.
func (e *Engine) WarmupAll(ctx context.Context, reason string) {
	// Only channels on the main model are warmed: it is the local one with a
	// cache to fill. A warmup of a cloud model costs money and buys nothing.
	var channels []config.Channel
	for _, ch := range []config.Channel{config.ChannelVoice, config.ChannelTelegram, config.ChannelBackground} {
		if e.cfg.RouteFor(ch) == config.MainModel {
			channels = append(channels, ch)
		}
	}
	if !e.cfg.LLM.SlotPinning && len(channels) > 1 {
		channels = channels[:1]
	}
	for _, ch := range channels {
		if ctx.Err() != nil {
			return
		}
		e.locks[ch].Lock()
		err := e.warmup(ctx, ch)
		e.locks[ch].Unlock()
		if err != nil {
			e.log.Warn("warmup failed", "channel", ch, "reason", reason, "error", err)
		}
	}
}

func (e *Engine) warmup(ctx context.Context, ch config.Channel) error {
	prefix, err := e.Prefix(ctx, ch)
	if err != nil {
		return err
	}
	built := prompt.WarmupRequest(prefix)
	req := e.chatRequest(ch, built.Messages, built)
	started := time.Now()
	client, _, _ := e.modelFor(config.MainModel)
	if err := client.Warmup(ctx, req); err != nil {
		return err
	}
	e.log.Info("slot warmed", "channel", ch, "prefix_hash", built.PrefixHash, "took", time.Since(started).Round(time.Millisecond))
	return nil
}
