package tools

import "testing"

// docs/spec/calendar.md, Steps 2, and docs/spec/threads.md: a tool of
// several skills.

func names(defs []string) map[string]bool {
	out := map[string]bool{}
	for _, d := range defs {
		out[d] = true
	}
	return out
}

func offered(r *Registry, skills ...string) map[string]bool {
	var n []string
	if len(skills) == 0 {
		for _, s := range r.Specs(false) {
			n = append(n, s.Name)
		}
	} else {
		for _, d := range r.ConversationSkillDefs(skills...) {
			n = append(n, d.Name)
		}
	}
	return names(n)
}

func TestCA08_TheToolComesWithItsSkillsOnly(t *testing.T) {
	r := NewRegistry(nil, InternalTool{Name: "calendar_create", Skills: []string{"postfach"}},
		InternalTool{Name: "calendar_events"})
	if offered(r)["calendar_create"] {
		t.Fatal("a turn without a skill is offered calendar_create")
	}
	if !offered(r)["calendar_events"] {
		t.Fatal("the shared tool is gone")
	}
	if !offered(r, "postfach")["calendar_create"] {
		t.Fatal("postfach does not get calendar_create")
	}
	if s, ok := r.Lookup("calendar_create"); !ok || !s.Skilled() || !s.Within([]string{"postfach"}) || s.Within([]string{"termin"}) {
		t.Fatalf("spec = %+v", s)
	}
}

func TestCA09_AToolOfSeveralSkills(t *testing.T) {
	r := NewRegistry(nil, InternalTool{Name: "x", Skills: []string{"a", "b"}})
	if !offered(r, "a")["x"] || !offered(r, "b")["x"] || offered(r, "c")["x"] {
		t.Fatal("not offered by its skills only")
	}
	s, _ := r.Lookup("x")
	if !s.Within([]string{"c", "b"}) || s.Within([]string{"c"}) {
		t.Fatalf("Within: %+v", s)
	}
}
