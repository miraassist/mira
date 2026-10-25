package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The context size comes from /models (any of the servers' field names,
// "models/" prefixes allowed), else from LM Studio's own list as loaded;
// a server that says nothing gives 0, for the user to fill in.
func TestContextSize(t *testing.T) {
	var auth, xkey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, xkey = r.Header.Get("Authorization"), r.Header.Get("x-api-key")
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
	if auth != "Bearer k" || xkey != "k" {
		t.Errorf("auth %q, x-api-key %q", auth, xkey)
	}
}
