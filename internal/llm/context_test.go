package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The context size comes from /models (any of the servers' field names,
// "models/" prefixes allowed), else from LM Studio's own list as loaded;
// a server that says nothing gives 0, for the user to fill in.
func TestContextSize(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/a/v1/models":
			io.WriteString(w, `{"data":[{"id":"x"},{"id":"claude","max_input_tokens":200000}]}`)
		case "/b/v1/models":
			io.WriteString(w, `{"data":[{"id":"models/flash","context_length":1048576}]}`)
		case "/c/v1/models":
			io.WriteString(w, `{"data":[{"id":"qwen","object":"model"}]}`)
		case "/c/api/v0/models":
			io.WriteString(w, `{"data":[{"id":"qwen","loaded_context_length":8192,"max_context_length":262144}]}`)
		case "/d/v1/models":
			io.WriteString(w, `{"data":[{"id":"gpt"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	for _, c := range []struct {
		base, model string
		want        int
		from        string
	}{
		{"/a/v1", "claude", 200000, ContextFromModels},
		{"/b/v1", "flash", 1048576, ContextFromModels},
		{"/c/v1", "qwen", 8192, ContextFromLMStudio},
		{"/d/v1", "gpt", 0, ""},
	} {
		n, from := ContextSize(ctx, srv.Client(), srv.URL+c.base, "k", c.model)
		if n != c.want || from != c.from {
			t.Errorf("%s %s: %d %q", c.base, c.model, n, from)
		}
	}
	if auth != "Bearer k" {
		t.Errorf("auth %q", auth)
	}
}

func TestListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a/v1/models":
			io.WriteString(w, `{"data":[{"id":"models/pro"},{"id":"models/flash"}]}`)
		case "/b/v1/models":
			io.WriteString(w, `{"data":[]}`)
		default:
			http.Error(w, "no key", http.StatusUnauthorized)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	got, err := ListModels(ctx, nil, srv.URL+"/a/v1/", "k")
	if err != nil || strings.Join(got, ",") != "flash,pro" {
		t.Errorf("list: %v %v", got, err)
	}
	if _, err := ListModels(ctx, nil, srv.URL+"/b/v1", "k"); err == nil {
		t.Error("an empty list is no list")
	}
	if _, err := ListModels(ctx, nil, srv.URL+"/c/v1", ""); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("refused: %v", err)
	}
}

func TestSetAuthAnthropicHeadersOnlyForAnthropic(t *testing.T) {
	for u, want := range map[string]string{"https://api.anthropic.com/v1/models": "k", "https://openrouter.ai/api/v1/models": ""} {
		req, _ := http.NewRequest(http.MethodGet, u, nil)
		SetAuth(req, "k")
		if req.Header.Get("Authorization") != "Bearer k" || req.Header.Get("x-api-key") != want ||
			(req.Header.Get("anthropic-version") != "") != (want != "") {
			t.Errorf("%s: %v", u, req.Header)
		}
	}
}

func TestProviderOf(t *testing.T) {
	cases := map[string]string{
		"https://api.openai.com/v1":     "openai",
		"https://api.anthropic.com/v1/": "anthropic",
		"http://192.0.2.1:1234/v1":      "",
		"":                              "",
	}
	for in, want := range cases {
		if got := ProviderOf(in); got != want {
			t.Errorf("ProviderOf(%q) = %q, want %q", in, got, want)
		}
	}
}
