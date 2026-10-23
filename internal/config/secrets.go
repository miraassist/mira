package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Secrets keeps the tokens entered on Mira's page out of mira.yaml
// (docs/spec/bearbeiten.md, Secrets): a file in the data folder, mode 0600,
// encrypted when MIRA_SECRET_KEY is set. mira.yaml refers to them as ${NAME}.
type Secrets struct {
	Path string
	// Key encrypts the file; empty keeps it plain.
	Key string
}

// SecretsFile is the name of the secrets file in the data folder.
const SecretsFile = "secrets.json"

// encPrefix marks an encrypted file.
const encPrefix = "mira-secrets:v1:"

// ErrSecretKey: the file is encrypted, and the key is missing or wrong.
var ErrSecretKey = errors.New("the secrets file is encrypted, and MIRA_SECRET_KEY is missing or wrong")

// DefaultSecrets is the secrets file in the data folder, with the key from
// the environment.
func DefaultSecrets() Secrets {
	return Secrets{Path: dataPath(SecretsFile), Key: os.Getenv("MIRA_SECRET_KEY")}
}

// Encrypted says whether the file is written encrypted.
func (s Secrets) Encrypted() bool { return s.Key != "" }

// Load reads the secrets; a missing file is none.
func (s Secrets) Load() (map[string]string, error) {
	raw, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read secrets: %w", err)
	}
	if rest, ok := strings.CutPrefix(string(raw), encPrefix); ok {
		if s.Key == "" {
			return nil, ErrSecretKey
		}
		if raw, err = s.open(strings.TrimSpace(rest)); err != nil {
			return nil, err
		}
	}
	out := map[string]string{}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("read secrets: %w", err)
	}
	return out, nil
}

// Set writes the given names; an empty value removes one.
func (s Secrets) Set(values map[string]string) error {
	all, err := s.Load()
	if err != nil {
		return err
	}
	for name, v := range values {
		if v == "" {
			delete(all, name)
		} else {
			all[name] = v
		}
	}
	raw, err := json.MarshalIndent(sorted(all), "", "  ")
	if err != nil {
		return err
	}
	if s.Key != "" {
		sealed, err := s.seal(raw)
		if err != nil {
			return err
		}
		raw = []byte(encPrefix + sealed + "\n")
	}
	return WriteAtomic(s.Path, raw, 0o600)
}

// Lookup is the secrets first, then base: a name the page wrote holds even
// when the environment has an older value. Without readable secrets it is
// base alone.
func (s Secrets) Lookup(base Lookup) Lookup {
	all, err := s.Load()
	if err != nil || len(all) == 0 {
		return base
	}
	return func(name string) (string, bool) {
		if v, ok := all[name]; ok {
			return v, true
		}
		return base(name)
	}
}

func (s Secrets) aead() (cipher.AEAD, error) {
	key := sha256.Sum256([]byte(s.Key))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (s Secrets) seal(plain []byte) (string, error) {
	gcm, err := s.aead()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, plain, nil)), nil
}

func (s Secrets) open(sealed string) ([]byte, error) {
	gcm, err := s.aead()
	if err != nil {
		return nil, err
	}
	data, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil || len(data) < gcm.NonceSize() {
		return nil, ErrSecretKey
	}
	plain, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], nil)
	if err != nil {
		return nil, ErrSecretKey
	}
	return plain, nil
}

// sorted keeps the file's names in order, for a stable file.
func sorted(m map[string]string) map[string]string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(map[string]string, len(m))
	for _, k := range keys {
		out[k] = m[k]
	}
	return out
}

// SecretName is the variable a secret of an entry is kept under:
// SecretName("MODEL", "smart", "TOKEN") is MIRA_MODEL_SMART_TOKEN.
func SecretName(parts ...string) string {
	var b strings.Builder
	b.WriteString("MIRA")
	for _, p := range parts {
		b.WriteByte('_')
		for _, r := range strings.ToUpper(p) {
			if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
				b.WriteRune(r)
			} else {
				b.WriteByte('_')
			}
		}
	}
	return b.String()
}

// WriteAtomic writes a file through a temporary one in the same folder, so a
// crash leaves the old file or the new one, never half of one.
func WriteAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
