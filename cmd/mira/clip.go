package main

import (
	"context"
	"strings"

	"github.com/miraassist/mira/internal/clip"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/tools"
)

// newClip builds clip_play (docs/spec/music-quiz.md) on Home Assistant:
// played after the answer, titled and watched as the answering machine's
// recordings are.
func (a *app) newClip() *clip.Player {
	return clip.New(clip.Options{
		Lang: a.lang, Logger: a.log,
		Satellite: func(ctx context.Context, deviceID string) clip.Satellite {
			s := a.voicemailSatellite(ctx, deviceID)
			return clip.Satellite{Speaker: s.Speaker, TitleAction: s.TitleAction, Entity: s.Entity}
		},
		AfterAnswer: func(entity string, fn func(ctx context.Context)) {
			go a.afterAnswer(entity, fn)
		},
		Title: func(ctx context.Context, action, title, subtitle, source string) error {
			domain, service, _ := strings.Cut(action, ".")
			return a.ha.CallService(ctx, domain, service, map[string]any{
				"title": title, "subtitle": subtitle, "source": source,
			}, nil)
		},
		Play: func(ctx context.Context, speaker, url string) error {
			return a.ha.CallService(ctx, "media_player", "play_media", map[string]any{
				"media_content_id": url, "media_content_type": "music",
			}, &haws.Target{EntityID: []string{speaker}})
		},
		Watch: a.playback,
		Stop: func(ctx context.Context, speaker string) error {
			return a.ha.CallService(ctx, "media_player", "media_stop", nil, &haws.Target{EntityID: []string{speaker}})
		},
		// The threads are built after the tools, so they are looked up when
		// used.
		Thread: func(ctx context.Context, inv tools.Invocation) string {
			if a.threads == nil {
				return ""
			}
			return a.threads.TurnThread(ctx, inv)
		},
		Note: func(ctx context.Context, threadID, text string) error {
			if a.threads == nil {
				return nil
			}
			return a.threads.Note(ctx, threadID, text)
		},
	})
}
