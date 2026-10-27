// Package setup lets the Mira integration set the agent up
// (docs/spec/einrichtung.md): pairing instead of typing a token (the first
// time without a code, later with one), and the Home Assistant access and the language model sent from Home
// Assistant instead of written into mira.yaml. What it gets is kept in the
// database and laid over the config at start.
package setup

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"strconv"
	"strings"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/store"
)

const (
	keyToken      = "setup.token"
	keyPaired     = "setup.paired"
	keyCode       = "setup.code"
	keyCodeTries  = "setup.code_tries"
	keyHAURL      = "setup.ha_url"
	keyHAToken    = "setup.ha_token"
	keyRejoin     = "setup.rejoin"
	keyLLM        = "setup.llm"
	codeLength    = 12
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
	// ContextTokens is the context the server said the model has, asked
	// once at setup; 0 when it did not say.
	ContextTokens int `json:"context_tokens,omitempty"`
}

// Settings is what setup keeps.
type Settings struct {
	// Token is the agent's access token when mira.yaml has none; made at
	// the first start.
	Token string
	// Paired: Home Assistant has the token. Open: it never had, so the
	// first pairing needs no code. Rejoin: the Home Assistant that let go
	// may pair again without a code (Unpair).
	Paired  bool
	Open    bool
	Rejoin  bool
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
	rejoin, err := get(ctx, st, keyRejoin)
	if err != nil {
		return s, err
	}
	s.Rejoin = rejoin != ""
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
		// The integration's model is main (docs/spec/modelle.md): a
		// provider is cloud, an own server local.
		main := cfg.Main()
		main.BaseURL, main.Model, main.APIKey = strings.TrimRight(l.BaseURL, "/"), l.Model, l.APIKey
		local := l.Kind != KindCloud
		main.Privacy, main.Parallel = config.PrivacyCloud, 4
		if local {
			main.Privacy, main.Parallel = config.PrivacyLocal, 1
		}
		main.PinSlots = nil
		if l.ContextTokens > 0 {
			main.ContextTokens = l.ContextTokens
		}
		main.CachePrompt = local
		main.WarmupOnStart, main.WarmupOnPrefixChange = local, local
		models := make(map[string]config.LLM, len(cfg.Models))
		for name, m := range cfg.Models {
			models[name] = m
		}
		models[config.MainModel] = main
		cfg.Models = models
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

// codeAlphabet leaves out what reads alike (0/O, 1/I/L).
const codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// newCode is a pairing code for another Home Assistant, made when one lets
// go (Unpair): twelve characters, about 59 bits, shown as XXXX-XXXX-XXXX.
// It holds until it is used; five wrong tries spend it.
func newCode(ctx context.Context, st *store.Store) (string, error) {
	b := make([]byte, codeLength)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(codeAlphabet))))
		if err != nil {
			return "", err
		}
		b[i] = codeAlphabet[n.Int64()]
	}
	code := string(b)
	for k, v := range map[string]string{keyCode: code, keyCodeTries: "0"} {
		if err := st.SetKV(ctx, k, v); err != nil {
			return "", err
		}
	}
	return code, nil
}

// ShowCode writes a code as it is read out: XXXX-XXXX-XXXX.
func ShowCode(code string) string {
	var parts []string
	for len(code) > 4 {
		parts, code = append(parts, code[:4]), code[4:]
	}
	return strings.Join(append(parts, code), "-")
}

// PendingCode is the code waiting to be used, if any.
func PendingCode(ctx context.Context, st *store.Store) (string, error) {
	return get(ctx, st, keyCode)
}

// ErrBadCode is a wrong or used up code.
var ErrBadCode = errors.New("wrong or used up code")

// Pair pairs and sets up in one step: Home Assistant's access and maybe a
// model are kept, and the token is handed out, or nothing happens at all.
// So no agent is left paired but unusable when a setup is broken off.
//
// An agent that was never paired needs no code: whoever pairs first wins,
// and the owner notices, because their own pairing then asks for a code.
// Later the code Unpair wrote into the log is needed; a wrong one counts,
// five spend it. The Home Assistant that let go needs none: it shows the
// rejoin key (RejoinKey of the token it had) instead.
func Pair(ctx context.Context, st *store.Store, code, rejoin string, haURL, haToken string, l *LLM) (string, error) {
	if haURL == "" || haToken == "" {
		return "", errors.New("pairing needs Home Assistant's address and token")
	}
	ok, err := rejoins(ctx, st, rejoin)
	if err != nil {
		return "", err
	}
	if !ok {
		if err := checkCode(ctx, st, code); err != nil {
			return "", err
		}
	}
	if err := Save(ctx, st, haURL, haToken, l); err != nil {
		return "", err
	}
	for _, k := range []string{keyCode, keyRejoin} {
		if err := st.DeleteKV(ctx, k); err != nil && !errors.Is(err, store.ErrNotFound) {
			return "", err
		}
	}
	if err := st.SetKV(ctx, keyPaired, "1"); err != nil {
		return "", err
	}
	return get(ctx, st, keyToken)
}

// checkCode lets a pairing through: without a code while the agent was
// never paired, else with the pending code.
func checkCode(ctx context.Context, st *store.Store, code string) error {
	paired, err := get(ctx, st, keyPaired)
	if err != nil {
		return err
	}
	code = strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(code))
	if paired == "" && code == "" {
		return nil
	}
	want, err := PendingCode(ctx, st)
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

// rejoins: the rejoin key shown matches the one Unpair kept.
func rejoins(ctx context.Context, st *store.Store, rejoin string) (bool, error) {
	if rejoin == "" {
		return false, nil
	}
	want, err := get(ctx, st, keyRejoin)
	if err != nil || want == "" {
		return false, err
	}
	return subtle.ConstantTimeCompare([]byte(rejoin), []byte(want)) == 1, nil
}

// RejoinKey is what the Home Assistant that let go shows to pair again
// without a code: an HMAC under the token it had, which it keeps.
func RejoinKey(token string) string {
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte("mira-rejoin"))
	return hex.EncodeToString(mac.Sum(nil))
}

// Unpair is for when Home Assistant lets go of the agent (the integration
// is removed): a new token, so the old one is worth nothing, and Home
// Assistant's access forgotten. The agent stays paired: the household's
// memory is still in it. The same Home Assistant pairs again without a
// code, with the rejoin key; any other needs the code returned here, which
// the agent writes into its log. The model stays.
func Unpair(ctx context.Context, st *store.Store) (string, error) {
	old, err := get(ctx, st, keyToken)
	if err != nil {
		return "", err
	}
	token, err := randomHex(tokenBytes)
	if err != nil {
		return "", err
	}
	if old != "" {
		if err := st.SetKV(ctx, keyRejoin, RejoinKey(old)); err != nil {
			return "", err
		}
	}
	if err := st.SetKV(ctx, keyToken, token); err != nil {
		return "", err
	}
	for _, k := range []string{keyHAURL, keyHAToken} {
		if err := st.DeleteKV(ctx, k); err != nil && !errors.Is(err, store.ErrNotFound) {
			return "", err
		}
	}
	return newCode(ctx, st)
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
