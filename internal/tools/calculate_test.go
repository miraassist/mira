package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/i18n"
)

// calculate computes exactly, with the usual precedence, and refuses what is
// no arithmetic.
func TestCalculate(t *testing.T) {
	for expr, want := range map[string]string{
		"307.93 + 276.77 + 54.86 + 108.29 + 88.06 + 317.75 + 853.77 + 94.9 + 66.07 + 478.38 + 2011.03 + 1558.59 + 374.95 + 396.75": "6988.1",
		"0.1 + 0.2":      "0.3",
		"2 + 3 * 4":      "14",
		"(2 + 3) * 4":    "20",
		"400 * 2 / 4":    "200",
		"10 / 3":         "3.333333",
		"-5 + 2":         "-3",
		"1.5 * -(2 - 4)": "3",
		" 1 +  1 ":       "2",
	} {
		if got, err := Calculate(expr); err != nil || got != want {
			t.Errorf("%q = %q %v, want %q", expr, got, err, want)
		}
	}
	for _, bad := range []string{"", "1 +", "2 * (3 + 4", "1 / 0", "1,5 + 2", "2 ^ 3", "1.2.3 + 1", "rm -rf"} {
		if got, err := Calculate(bad); err == nil {
			t.Errorf("%q = %q, want an error", bad, got)
		}
	}
	// As a tool: read only, and an error says what is wrong.
	var tool InternalTool
	for _, it := range Internal(i18n.DE, nil, nil) {
		if it.Name == ToolCalculate {
			tool = it
		}
	}
	if !tool.ReadOnly {
		t.Fatal("calculate is missing or not read only")
	}
	if out, err := tool.Run(context.Background(), Invocation{Args: json.RawMessage(`{"expression":"1 + 2"}`)}); out != "3" || err != nil {
		t.Errorf("tool: %q %v", out, err)
	}
	if _, err := tool.Run(context.Background(), Invocation{Args: json.RawMessage(`{"expression":"1 / 0"}`)}); err == nil ||
		!strings.Contains(err.Error(), "nicht ausrechnen") {
		t.Errorf("tool error: %v", err)
	}
}
