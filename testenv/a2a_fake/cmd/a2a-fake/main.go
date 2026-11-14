// Command a2a-fake runs the fake A2A agent (testenv/a2a_fake) for the test
// environment: the container a2a-fake in testenv/compose.yaml.
package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"
	"time"

	a2afake "github.com/miraassist/mira/testenv/a2a_fake"
)

func main() {
	listen := flag.String("listen", ":8080", "address to listen on")
	token := flag.String("token", a2afake.DefaultToken, "the bearer token, API key or OAuth2 token the fake accepts")
	wait := flag.Duration("wait", 0, "how long the skill slow works; 0: until POST /fake/release")
	flag.Parse()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	f := a2afake.New(a2afake.Options{Token: *token, Wait: *wait, Logger: log})
	log.Info("a2a fake listening", "listen", *listen)
	srv := &http.Server{Addr: *listen, Handler: f, ReadHeaderTimeout: 10 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		log.Error("a2a fake stopped", "error", err)
		os.Exit(1)
	}
}
