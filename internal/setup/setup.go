// Package setup lets the Mira integration set the agent up
// (docs/spec/einrichtung.md): pairing instead of typing a token (the first
// time without a code, later with one), and the Home Assistant access and the language model sent from Home
// Assistant instead of written into mira.yaml. What it gets is kept in the
// database and laid over the config at start.
package setup

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/store"
)

const (
	keyToken      = "setup.token"
	keyPaired     = "setup.paired"
	keyCode       = "setup.code"
	keyCodeUntil  = "setup.code_until"
	keyCodeTries  = "setup.code_tries"
	keyHAURL      = "setup.ha_url"
	keyHAToken    = "setup.ha_token"
	keyLLM        = "setup.llm"
	codeLifetime  = 15 * time.Minute
	codeMaxTries  = 5
	tokenBytes    = 32
	haMCPServer   = "ha"
	haMCPEndpoint = "/api/mcp/assist"
)

// Kinds of model servers: a cloud provider, or a server of one's own.
// Only one's own is warmed at start and gets cache_prompt; a cloud is
// billed per token and may refuse fields it does not know.
const (
	KindCloud  = "cloud"
	KindCustom = "custom"
)

// LLM is the language model as the setup form sets it.
type LLM struct {
	Kind    string `json:"kind"`
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
	APIKey  string `json:"api_key,omitempty"`
}

// Settings is what setup keeps.
type Settings struct {
	// Token is the agent's access token when mira.yaml has none; made at
	// the first start.
	Token string
	// Paired: Home Assistant has the token. Open: it never had, so the
	// first pairing needs no code.
	Paired  bool
	Open    bool
	HAURL   string
	HAToken string
	LLM     *LLM
}

// Load reads what setup keeps, making the token on the first start.
func Load(ctx context.Context, st *store.Store) (Settings, error) {
	var s Settings
	var err error
	if s.Token, err = get(ctx, st, keyToken); err != nil {
		return s, err
	}
	if s.Token == "" {
		if s.Token, err = randomHex(tokenBytes); err != nil {
			return s, err
		}
		if err := st.SetKV(ctx, keyToken, s.Token); err != nil {
			return s, err
		}
	}
	paired, err := get(ctx, st, keyPaired)
	if err != nil {
		return s, err
	}
	s.Paired = paired == "1"
	s.Open = paired == ""
	if s.HAURL, err = get(ctx, st, keyHAURL); err != nil {
		return s, err
	}
	if s.HAToken, err = get(ctx, st, keyHAToken); err != nil {
		return s, err
	}
	raw, err := get(ctx, st, keyLLM)
	if err != nil {
		return s, err
	}
	if raw != "" {
		var l LLM
		if json.Unmarshal([]byte(raw), &l) == nil {
			s.LLM = &l
		}
	}
	return s, nil
}

// Apply lays the settings over the config: the token when the file has
// none, the Home Assistant access and its MCP server, and the language
// model. What setup sent wins over the file (docs/spec/einrichtung.md).
func Apply(cfg config.Config, s Settings) config.Config {
	if cfg.Server.Token == "" {
		cfg.Server.Token = s.Token
	}
	if s.HAURL != "" && s.HAToken != "" {
		cfg.HA.URL, cfg.HA.Token = strings.TrimRight(s.HAURL, "/"), s.HAToken
	}
	if cfg.HA.URL != "" && cfg.HA.Token != "" && !hasServer(cfg, haMCPServer) {
		cfg.MCPServers = append([]config.MCPServer{{Name: haMCPServer, Transport: config.TransportHTTP,
			URL: cfg.HA.URL + haMCPEndpoint, Token: cfg.HA.Token}}, cfg.MCPServers...)
	}
	if l := s.LLM; l != nil {
		cfg.LLM.BaseURL, cfg.LLM.Model, cfg.LLM.APIKey = strings.TrimRight(l.BaseURL, "/"), l.Model, l.APIKey
		local := l.Kind != KindCloud
		cfg.LLM.SlotPinning = false
		cfg.LLM.CachePrompt = local
		cfg.LLM.WarmupOnStart, cfg.LLM.WarmupOnPrefixChange = local, local
	}
	return cfg
}

func hasServer(cfg config.Config, name string) bool {
	for _, s := range cfg.MCPServers {
		if s.Name == name {
			return true
		}
	}
	return false
}

// Ready reports whether the agent can run: Home Assistant is reachable and
// a language model is set, in the file or by setup.
func Ready(cfg config.Config, fileHasLLM bool, s Settings) bool {
	return cfg.HA.URL != "" && cfg.HA.Token != "" && (fileHasLLM || s.LLM != nil)
}

// Save keeps what the integration sent; nil parts stay as they were.
func Save(ctx context.Context, st *store.Store, haURL, haToken string, l *LLM) error {
	if haURL != "" && haToken != "" {
		if err := st.SetKV(ctx, keyHAURL, strings.TrimRight(haURL, "/")); err != nil {
			return err
		}
		if err := st.SetKV(ctx, keyHAToken, haToken); err != nil {
			return err
		}
	}
	if l != nil {
		raw, err := json.Marshal(l)
		if err != nil {
			return err
		}
		if err := st.SetKV(ctx, keyLLM, string(raw)); err != nil {
			return err
		}
	}
	return nil
}

// NewCode starts pairing anew (mira pair): a code of six digits that holds
// 15 minutes and five tries. The old pairing stays until the code is used.
func NewCode(ctx context.Context, st *store.Store, now time.Time) (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	code := fmt.Sprintf("%06d", n.Int64())
	for k, v := range map[string]string{
		keyCode: code, keyCodeUntil: now.Add(codeLifetime).UTC().Format(time.RFC3339), keyCodeTries: "0",
	} {
		if err := st.SetKV(ctx, k, v); err != nil {
			return "", err
		}
	}
	return code, nil
}

// PendingCode is the code waiting to be used, if any.
func PendingCode(ctx context.Context, st *store.Store, now time.Time) (string, error) {
	code, err := get(ctx, st, keyCode)
	if err != nil || code == "" {
		return "", err
	}
	until, err := get(ctx, st, keyCodeUntil)
	if err != nil {
		return "", err
	}
	t, err := time.Parse(time.RFC3339, until)
	if err != nil || now.After(t) {
		return "", nil
	}
	return code, nil
}

// ErrBadCode is a wrong, used up or expired code.
var ErrBadCode = errors.New("wrong or expired code")

// Pair pairs and sets up in one step: Home Assistant's access and maybe a
// model are kept, and the token is handed out, or nothing happens at all.
// So no agent is left paired but unusable when a setup is broken off.
//
// An agent that was never paired needs no code: whoever pairs first wins,
// and the owner notices, because their own pairing then asks for a code.
// Later a code from mira pair is needed; a wrong one counts, five spend it.
func Pair(ctx context.Context, st *store.Store, code string, now time.Time, haURL, haToken string, l *LLM) (string, error) {
	if haURL == "" || haToken == "" {
		return "", errors.New("pairing needs Home Assistant's address and token")
	}
	if err := checkCode(ctx, st, code, now); err != nil {
		return "", err
	}
	if err := Save(ctx, st, haURL, haToken, l); err != nil {
		return "", err
	}
	if err := st.DeleteKV(ctx, keyCode); err != nil {
		return "", err
	}
	if err := st.SetKV(ctx, keyPaired, "1"); err != nil {
		return "", err
	}
	return get(ctx, st, keyToken)
}

// checkCode lets a pairing through: without a code while the agent was
// never paired, else with the pending code.
func checkCode(ctx context.Context, st *store.Store, code string, now time.Time) error {
	paired, err := get(ctx, st, keyPaired)
	if err != nil {
		return err
	}
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if paired == "" && code == "" {
		return nil
	}
	want, err := PendingCode(ctx, st, now)
	if err != nil {
		return err
	}
	if want == "" {
		return ErrBadCode
	}
	triesRaw, err := get(ctx, st, keyCodeTries)
	if err != nil {
		return err
	}
	tries, _ := strconv.Atoi(triesRaw)
	tries++
	if err := st.SetKV(ctx, keyCodeTries, strconv.Itoa(tries)); err != nil {
		return err
	}
	if subtle.ConstantTimeCompare([]byte(code), []byte(want)) != 1 {
		if tries >= codeMaxTries {
			_ = st.DeleteKV(ctx, keyCode)
		}
		return ErrBadCode
	}
	return nil
}

// Unpair is for when Home Assistant lets go of the agent (the integration
// is removed): a new token, so the old one is worth nothing, Home
// Assistant's access forgotten, and the next pairing again without a code.
// The model stays.
func Unpair(ctx context.Context, st *store.Store) error {
	token, err := randomHex(tokenBytes)
	if err != nil {
		return err
	}
	if err := st.SetKV(ctx, keyToken, token); err != nil {
		return err
	}
	for _, k := range []string{keyPaired, keyCode, keyHAURL, keyHAToken} {
		if err := st.DeleteKV(ctx, k); err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
	}
	return nil
}

func get(ctx context.Context, st *store.Store, key string) (string, error) {
	v, err := st.GetKV(ctx, key)
	if errors.Is(err, store.ErrNotFound) {
		return "", nil
	}
	return v, err
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
