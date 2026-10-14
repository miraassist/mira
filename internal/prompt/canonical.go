// Package prompt assembles every request to the model out of fixed layers.
//
// llama.cpp reuses its KV cache only for an identical prompt prefix, so layers
// 1 to 5 must be byte-identical for identical inputs. Everything in this
// package exists to make that true and to make a violation visible: no map
// iteration, no timestamps, no ids, no randomness above layer 7, and a hash
// over the stable layers that travels with every request as telemetry.
package prompt

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
)

// CanonicalJSON rewrites a JSON document into a single deterministic form:
// object keys sorted, no insignificant whitespace, numbers normalised.
//
// Tool schemas arrive from MCP servers as arbitrary JSON. Two servers may send
// semantically identical schemas with different key order or spacing; without
// this the prompt prefix would differ between restarts and the cache would be
// lost for no reason.
func CanonicalJSON(raw []byte) ([]byte, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return []byte("{}"), nil
	}
	var v any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // keep 1 and 1.0 distinguishable instead of both becoming float64
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("canonicalise json: %w", err)
	}
	var buf bytes.Buffer
	if err := writeCanonical(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeCanonical(buf *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeJSONString(buf, k); err != nil {
				return err
			}
			buf.WriteByte(':')
			if err := writeCanonical(buf, t[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	case []any:
		// Array order is meaningful (think "enum" or "required"), so it stays.
		buf.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeCanonical(buf, e); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case string:
		return writeJSONString(buf, t)
	case json.Number:
		buf.WriteString(normaliseNumber(t.String()))
	case bool:
		buf.WriteString(strconv.FormatBool(t))
	case nil:
		buf.WriteString("null")
	default:
		return fmt.Errorf("canonicalise json: unexpected type %T", v)
	}
	return nil
}

func writeJSONString(buf *bytes.Buffer, s string) error {
	// encoding/json escapes <, > and & by default, which is deterministic but
	// noisy. Disabling it keeps schemas readable; either way it is stable.
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return err
	}
	buf.Truncate(buf.Len() - 1) // Encode appends a newline
	return nil
}

// normaliseNumber removes representation differences that carry no meaning,
// so 1.0, 1 and 1e0 all serialise the same way.
func normaliseNumber(s string) string {
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return strconv.FormatInt(i, 10)
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return s
	}
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// hashLayers produces the prefix hash. It is a short hex string so it reads
// well as a span attribute and in logs; 64 bits is far more than enough to
// notice an accidental change.
func hashLayers(layers ...string) string {
	h := sha256.New()
	for _, l := range layers {
		// The length prefix keeps concatenation unambiguous: two different
		// splits of the same bytes must not hash the same.
		fmt.Fprintf(h, "%d:", len(l))
		h.Write([]byte(l))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
