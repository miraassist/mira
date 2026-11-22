package llm_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/clock"
	"github.com/miraassist/mira/internal/llm"
	"github.com/miraassist/mira/internal/retry"
)

// Retries of the model client: docs/spec/retry.md, step 4.

const okAnswer = `{"id":"x","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{}}`

// busyOnce answers the first request with 429 as busy says, then ok; it
// counts the requests.
func busyOnce(t *testing.T, busy func(http.ResponseWriter)) (*httptest.Server, *atomic.Int32) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if n.Add(1) == 1 {
			busy(w)
			return
		}
		w.Write([]byte(okAnswer))
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

func chat(ctx context.Context, url string, clk clock.Clock) error {
	c := llm.New(llm.Options{BaseURL: url + "/v1", Model: "m", MaxRetries: 2, Clock: clk})
	_, err := c.Chat(ctx, llm.ChatRequest{Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}})
	return err
}

func fakeClock() *clock.Fake { return clock.NewFake(time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)) }

// waitedBefore runs a chat and says how long it waited before its second
// attempt, on the clock the test moves.
func waitedBefore(t *testing.T, url string) (time.Duration, error) {
	t.Helper()
	clk := fakeClock()
	done := make(chan error, 1)
	go func() { done <- chat(context.Background(), url, clk) }()
	<-clk.AwaitWaiters(1)
	waited := clk.AdvanceToNext()
	return waited, <-done
}

func TestRY30_TheModelsNamedWaitIsKept(t *testing.T) {
	srv, n := busyOnce(t, func(w http.ResponseWriter) {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"slow down"}`))
	})
	waited, err := waitedBefore(t, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if n.Load() != 2 || waited != time.Second {
		t.Fatalf("%d attempts, waited %s", n.Load(), waited)
	}
}

func TestRY31_GooglesRetryDelayIsReadFromTheBody(t *testing.T) {
	srv, n := busyOnce(t, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`[{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"1s"}]}}]`))
	})
	waited, err := waitedBefore(t, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if n.Load() != 2 || waited != time.Second {
		t.Fatalf("%d attempts, waited %s", n.Load(), waited)
	}
}

func TestRY32_ANamedWaitLongerThanTheCallersDeadlineFailsAtOnce(t *testing.T) {
	srv, n := busyOnce(t, func(w http.ResponseWriter) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	clk := fakeClock()
	ctx, cancel := clock.WithTimeout(context.Background(), clk, 5*time.Second)
	defer cancel()
	err := chat(ctx, srv.URL, clk)
	var long *retry.WaitTooLong
	if !errors.As(err, &long) || n.Load() != 1 || clk.Waiters() != 1 { // only the caller's deadline waits
		t.Fatalf("err = %v, %d attempts, %d waits", err, n.Load(), clk.Waiters())
	}
	if !strings.Contains(err.Error(), "asked to wait 30s") || !strings.Contains(err.Error(), "429") {
		t.Errorf("error = %q", err)
	}
}
