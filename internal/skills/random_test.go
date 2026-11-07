package skills

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

const quizAxes = `    random:
      alter: [Kinder, Erwachsene]
      schwierigkeit: [leicht, mittel, schwer]
      thema:
        Tiere: [Meerestiere, Vögel, Insekten]
        Geografie: [Flüsse, Gebirge]
      frageart: [Wer, Wo, Wie viele, Richtig oder falsch]
`

// writeRandomSkill writes a skill "spiel" with the given random block under
// metadata.mira and, when en is set, a translation with that frontmatter.
func writeRandomSkill(t *testing.T, random, en string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "spiel")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	skill := "---\nname: spiel\ndescription: Ein Spiel.\nmetadata:\n  mira:\n" + random + "---\n# Spiel\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skill), 0o644); err != nil {
		t.Fatal(err)
	}
	if en != "" {
		if err := os.WriteFile(filepath.Join(dir, "SKILL.en.md"), []byte("---\ndescription: A game.\n"+en+"---\n# Game\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func loadRandom(t *testing.T, random, en string) Skill {
	t.Helper()
	sk, p := load(writeRandomSkill(t, random, en), nil)
	if p != nil {
		t.Fatalf("load: %v", p)
	}
	return sk
}

func pickTool(t *testing.T, list ...Skill) tools.InternalTool {
	t.Helper()
	got := RandomTools(i18n.DE, list)
	if len(got) != 1 || got[0].Name != ToolRandomPick {
		t.Fatalf("tools: %+v", got)
	}
	return got[0]
}

func runPick(ctx context.Context, tool tools.InternalTool, args string) ([]string, error) {
	out, err := tool.Run(ctx, tools.Invocation{Args: json.RawMessage(args)})
	if err != nil {
		return nil, err
	}
	return strings.Split(out, "\n"), nil
}

// field is an axis's value in a line "1. a: x · b: y".
func field(line, axis string) string {
	_, rest, _ := strings.Cut(line, ". ")
	for _, part := range strings.Split(rest, " · ") {
		if v, ok := strings.CutPrefix(part, axis+": "); ok {
			return v
		}
	}
	return ""
}

func TestRP01_AxesInTheirOrder(t *testing.T) {
	sk := loadRandom(t, quizAxes, "")
	var names []string
	for _, a := range sk.Random {
		names = append(names, a.Name)
	}
	if strings.Join(names, ",") != "alter,schwierigkeit,thema,frageart" {
		t.Fatalf("axes %v", names)
	}
	thema := sk.Random[2]
	if strings.Join(thema.Values, ",") != "Tiere,Geografie" || strings.Join(thema.Subs["Geografie"], ",") != "Flüsse,Gebirge" {
		t.Errorf("map axis %+v", thema)
	}
	if sk.Random[0].Subs != nil {
		t.Error("a list axis has sub-values")
	}
}

func TestRP02_BrokenAxesAreAProblem(t *testing.T) {
	for name, c := range map[string]struct{ random, kind string }{
		"empty block":   {"    random: {}\n", "randomnone"},
		"empty list":    {"    random:\n      thema: []\n", "randomempty"},
		"list in list":  {"    random:\n      thema: [[a, b]]\n", "randomaxis"},
		"a single text": {"    random:\n      thema: Tiere\n", "randomaxis"},
		"no sub-values": {"    random:\n      thema:\n        Tiere: []\n", "randomempty"},
		"too deep":      {"    random:\n      thema:\n        Tiere:\n          Vögel: [Spatz]\n", "randomaxis"},
	} {
		_, p := load(writeRandomSkill(t, c.random, ""), nil)
		if p == nil || p.kind != c.kind {
			t.Errorf("%s: %v", name, p)
			continue
		}
		if txt := p.Text(i18n.DE); !strings.HasPrefix(txt, "SKILL.md:") {
			t.Errorf("%s: the problem does not name the file: %s", name, txt)
		}
	}
}

func TestRP03_TranslationBringsItsAxes(t *testing.T) {
	sk := loadRandom(t, quizAxes, "random:\n  age: [children, adults]\n  difficulty: [easy, hard]\n")
	tool := pickTool(t, sk)
	lines, err := runPick(i18n.WithLang(context.Background(), i18n.EN), tool, `{"skill":"spiel","count":2}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range lines {
		if field(l, "age") == "" || field(l, "difficulty") == "" || field(l, "thema") != "" {
			t.Errorf("english line %q", l)
		}
	}
	lines, _ = runPick(i18n.WithLang(context.Background(), i18n.DE), tool, `{"skill":"spiel"}`)
	if field(lines[0], "thema") == "" {
		t.Errorf("german line %q", lines[0])
	}
}

func TestRP04_BrokenTranslatedAxesAreAProblem(t *testing.T) {
	_, p := load(writeRandomSkill(t, quizAxes, "random:\n  age: []\n"), nil)
	if p == nil || p.kind != "randomempty" || !strings.HasPrefix(p.Text(i18n.EN), "SKILL.en.md:") {
		t.Fatalf("problem %v", p)
	}
}

func TestRP10_OnlyFreeAxesAreDrawn(t *testing.T) {
	tool := pickTool(t, loadRandom(t, quizAxes, ""))
	lines, err := runPick(context.Background(), tool, `{"skill":"spiel","fixed":{"Alter":"Erwachsene","schwierigkeit":"schwer"},"count":3}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 {
		t.Fatalf("lines %v", lines)
	}
	for i, l := range lines {
		if !strings.HasPrefix(l, string(rune('1'+i))+". ") {
			t.Errorf("line %d not numbered: %q", i+1, l)
		}
		if field(l, "alter") != "Erwachsene" || field(l, "schwierigkeit") != "schwer" ||
			!strings.Contains(field(l, "thema"), " / ") || field(l, "frageart") == "" {
			t.Errorf("line %q", l)
		}
	}
}

func TestRP11_AValueComesAgainOnlyAfterAll(t *testing.T) {
	axes := []Axis{{Name: "farbe", Values: []string{"rot", "grün", "blau", "gelb"}}}
	for run := 0; run < 50; run++ {
		lines, err := Pick(axes, nil, 8)
		if err != nil {
			t.Fatal(err)
		}
		for _, half := range [][]string{lines[:4], lines[4:]} {
			seen := map[string]bool{}
			for _, l := range half {
				seen[field(l, "farbe")] = true
			}
			if len(seen) != 4 {
				t.Fatalf("a value came again before all were drawn: %v", lines)
			}
		}
	}
}

func TestRP12_FixedMapValueDrawsItsSubValue(t *testing.T) {
	sk := loadRandom(t, quizAxes, "")
	lines, err := Pick(sk.Random, map[string]string{"thema": "tiere"}, 3)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, l := range lines {
		v := field(l, "thema")
		sub, ok := strings.CutPrefix(v, "Tiere / ")
		if !ok {
			t.Fatalf("line %q", l)
		}
		seen[sub] = true
	}
	if len(seen) != 3 {
		t.Errorf("the three subtopics were not each drawn once: %v", lines)
	}
}

func TestRP13_FixedSubValueOrUnknownIsKept(t *testing.T) {
	sk := loadRandom(t, quizAxes, "")
	for fixed, want := range map[string]string{"Vögel": "Tiere / Vögel", "Harry Potter": "Harry Potter"} {
		lines, err := Pick(sk.Random, map[string]string{"thema": fixed}, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range lines {
			if field(l, "thema") != want {
				t.Errorf("%s: line %q", fixed, l)
			}
		}
	}
}

func TestRP14_WrongCallsAreToldToTheModel(t *testing.T) {
	without := loadRandom(t, "", "")
	without.Name = "ohne"
	tool := pickTool(t, loadRandom(t, quizAxes, ""), without)
	for args, want := range map[string]string{
		`{"skill":"gibtsnicht"}`:              `keinen Skill "gibtsnicht"`,
		`{"skill":"ohne"}`:                    `"ohne" hat keine Achsen`,
		`{"skill":"spiel","fixed":{"x":"1"}}`: `keine Achse "x"; seine Achsen: alter, schwierigkeit, thema, frageart`,
		`{"skill":"spiel","count":0}`:         "zwischen 1 und 30",
		`{"skill":"spiel","count":31}`:        "zwischen 1 und 30",
	} {
		_, err := runPick(context.Background(), tool, args)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", args, err)
		}
	}
}

func TestRP15_NoToolWithoutAxes(t *testing.T) {
	if got := RandomTools(i18n.DE, []Skill{loadRandom(t, "", "")}); got != nil {
		t.Errorf("tools %+v", got)
	}
}

// repoSkill loads one of the repository's skills.
func repoSkill(t *testing.T, name string) Skill {
	t.Helper()
	list, problems := Load([]string{"../../skills"}, map[string]map[string]any{name: {}})
	if len(problems) != 0 || len(list) != 1 {
		t.Fatalf("load %s: %v", name, problems)
	}
	return list[0]
}

// checkRepoAxes checks a repository skill's axes by language, that a draw
// over them works, and that its guide calls random_pick.
func checkRepoAxes(t *testing.T, name string, want map[i18n.Lang]string) {
	t.Helper()
	sk := repoSkill(t, name)
	tool := pickTool(t, sk)
	for l, axes := range want {
		var names []string
		for _, a := range sk.AxesIn(l) {
			names = append(names, a.Name)
		}
		if strings.Join(names, ",") != axes {
			t.Errorf("%s %s: axes %v", name, l, names)
		}
		lines, err := runPick(i18n.WithLang(context.Background(), l), tool, `{"skill":"`+name+`","count":10}`)
		if err != nil || len(lines) != 10 {
			t.Errorf("%s %s: draw %v %v", name, l, lines, err)
		}
		if !strings.Contains(sk.BodyIn(l), "`random_pick`") {
			t.Errorf("%s %s: the guide does not call random_pick", name, l)
		}
	}
}

func TestRP20_QuizDrawsItsQuestions(t *testing.T) {
	checkRepoAxes(t, "quiz", map[i18n.Lang]string{
		i18n.DE: "schwierigkeit,thema,frageart",
		i18n.EN: "difficulty,topic,question",
	})
}

func TestRP21_VocabularyDrawsItsWordField(t *testing.T) {
	checkRepoAxes(t, "vokabeln", map[i18n.Lang]string{
		i18n.DE: "niveau,wortfeld",
		i18n.EN: "level,field",
	})
}

func TestRP22_MusicQuizDrawsItsYears(t *testing.T) {
	checkRepoAxes(t, "musikquiz", map[i18n.Lang]string{
		i18n.DE: "zeit,herkunft",
		i18n.EN: "era,origin",
	})
	lines, err := Pick(repoSkill(t, "musikquiz").Random, map[string]string{"zeit": "80er"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	years := map[string]bool{}
	for _, l := range lines {
		years[field(l, "zeit")] = true
	}
	if len(years) != 10 || !years["80er / 1983"] {
		t.Errorf("ten songs of the 80s are not ten different years: %v", lines)
	}
}

func TestRP23_StoryDrawsItsHero(t *testing.T) {
	checkRepoAxes(t, "geschichte", map[i18n.Lang]string{i18n.DE: "held,ort,problem,gegenstand"})
}
