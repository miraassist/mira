package wifi

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// RY-52, docs/spec/retry.md step 6.
func TestRY52_AFailedAnswerOfTheSupervisorWhileJoiningIsLogged(t *testing.T) {
	var mu sync.Mutex
	infos := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			fmt.Fprint(w, `{"result":"ok","data":{}}`)
			return
		}
		mu.Lock()
		infos++
		n := infos
		mu.Unlock()
		switch {
		case n == 2 || n == 3: // after the join: the Supervisor fails twice
			http.Error(w, `{"result":"error","message":"busy"}`, http.StatusInternalServerError)
		default:
			fmt.Fprintf(w, `{"result":"ok","data":{"interfaces":[{"interface":"wlan0","type":"wireless","enabled":true,"connected":%t}]}}`, n > 3)
		}
	}))
	defer srv.Close()
	var logs bytes.Buffer
	sv := &Supervisor{Base: srv.URL, Token: "t", Client: srv.Client(), Log: slog.New(slog.NewTextHandler(&logs, nil))}
	if _, err := sv.Join(context.Background(), "shelly-ap", ""); err != nil {
		t.Fatal(err)
	}
	if out := logs.String(); strings.Count(out, "level=WARN") != 1 || !strings.Contains(out, "ssid=shelly-ap") {
		t.Errorf("log = %s", out)
	}
}
