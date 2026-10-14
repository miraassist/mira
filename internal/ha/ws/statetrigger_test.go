package ws

import "testing"

func TestStateTriggerMatches(t *testing.T) {
	cases := []struct {
		name     string
		trigger  stateTrigger
		from, to string
		want     bool
	}{
		{"any change", stateTrigger{}, "off", "on", true},
		{"no change", stateTrigger{}, "on", "on", false},
		{"to matches", stateTrigger{to: []string{"fault"}}, "ok", "fault", true},
		{"to does not match", stateTrigger{to: []string{"fault"}}, "fault", "ok", false},
		{"from does not match", stateTrigger{from: []string{"ok"}}, "unavailable", "fault", false},
		{"not_to excluded", stateTrigger{notTo: []string{"normal", "unavailable"}}, "normal", "unavailable", false},
		{"not_to allowed", stateTrigger{notTo: []string{"normal", "unavailable"}}, "normal", "fault", true},
		{"not_from excluded", stateTrigger{notFrom: []string{"unavailable"}}, "unavailable", "on", false},
	}
	for _, c := range cases {
		if got := c.trigger.matches(c.from, c.to); got != c.want {
			t.Errorf("%s: matches(%q, %q) = %v", c.name, c.from, c.to, got)
		}
	}
}

func TestParseStateTrigger(t *testing.T) {
	st, err := parseStateTrigger(map[string]any{"trigger": "state", "entity_id": []any{"a.b", "c.d"}, "not_to": "x"})
	if err != nil || len(st.entityIDs) != 2 || st.notTo[0] != "x" {
		t.Fatalf("st = %+v, err = %v", st, err)
	}
	if _, err := parseStateTrigger(map[string]any{"trigger": "state"}); err == nil {
		t.Error("trigger without entity accepted")
	}
}
