package llm_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/llm"
	"github.com/miraassist/mira/internal/retry"
)

// Retries of the model client: docs/spec/retry.md, step 4.

const okAnswer = `{"id":"x","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{}}`

// busyOnce answers the first request with 429 as busy says, then ok; it
// keeps the times of the requests.
func busyOnce(t *testing.T, busy func(http.ResponseWriter)) (*httptest.Server, *[]time.Time) {
	var times []time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		times = append(times, time.Now())
		if len(times) == 1 {
			busy(w)
			return
		}
		w.Write([]byte(okAnswer))
	}))
	t.Cleanup(srv.Close)
	return srv, &times
}

func chat(ctx context.Context, url string) error {
	c := llm.New(llm.Options{BaseURL: url + "/v1", Model: "m", MaxRetries: 2})
	_, err := c.Chat(ctx, llm.ChatRequest{Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}})
	return err
}

func TestRY30_TheModelsNamedWaitIsKept(t *testing.T) {
	srv, times := busyOnce(t, func(w http.ResponseWriter) {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"slow down"}`))
	})
	if err := chat(context.Background(), srv.URL); err != nil {
		t.Fatal(err)
	}
	if len(*times) != 2 || (*times)[1].Sub((*times)[0]) < time.Second {
		t.Fatalf("attempts %v", *times)
	}
}

func TestRY31_GooglesRetryDelayIsReadFromTheBody(t *testing.T) {
	srv, times := busyOnce(t, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`[{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"1s"}]}}]`))
	})
	if err := chat(context.Background(), srv.URL); err != nil {
		t.Fatal(err)
	}
	if len(*times) != 2 || (*times)[1].Sub((*times)[0]) < time.Second {
		t.Fatalf("attempts %v", *times)
	}
}

func TestRY32_ANamedWaitLongerThanTheCallersDeadlineFailsAtOnce(t *testing.T) {
	srv, times := busyOnce(t, func(w http.ResponseWriter) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	began := time.Now()
	err := chat(ctx, srv.URL)
	var long *retry.WaitTooLong
	if !errors.As(err, &long) || len(*times) != 1 || time.Since(began) > time.Second {
		t.Fatalf("err = %v, %d attempts, took %s", err, len(*times), time.Since(began))
	}
	if !strings.Contains(err.Error(), "asked to wait 30s") || !strings.Contains(err.Error(), "429") {
		t.Errorf("error = %q", err)
	}
}
