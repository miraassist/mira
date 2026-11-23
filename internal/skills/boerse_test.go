package skills_test

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/startools"
)

// The shipped skill boerse (docs/spec/boerse.md).

// yahooFake answers Yahoo's search and chart and records the paths asked.
type yahooFake struct {
	search string
	charts map[string]string
	status int
	asked  []string
}

func (f *yahooFake) Request(_ context.Context, _, _, raw string, _ []byte) (int, []byte, error) {
	u, _ := url.Parse(raw)
	f.asked = append(f.asked, u.Path)
	if f.status != 0 {
		return f.status, []byte("Too Many Requests"), nil
	}
	if u.Path == "/v1/finance/search" {
		return 200, []byte(f.search), nil
	}
	sym, _ := url.PathUnescape(strings.TrimPrefix(u.Path, "/v8/finance/chart/"))
	if c, ok := f.charts[sym]; ok {
		return 200, []byte(c), nil
	}
	return 404, []byte(`{"chart":{"result":null,"error":{"code":"Not Found"}}}`), nil
}

func runKurs(t *testing.T, f *yahooFake, wert string) (map[string]any, error) {
	t.Helper()
	loaded, problems := skills.Load([]string{"../../skills"}, map[string]map[string]any{"boerse": {}})
	if len(problems) > 0 || len(loaded) != 1 {
		t.Fatalf("boerse did not load: %v", problems)
	}
	if loaded[0].OAuth != nil || len(loaded[0].API) != 1 {
		t.Fatalf("boerse: oauth %v api %v", loaded[0].OAuth, loaded[0].API)
	}
	args, _ := json.Marshal(map[string]string{"wert": wert})
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	res := loaded[0].Starlark.Call(context.Background(), "kurs", args, startools.Env{Requester: f, Now: func() time.Time { return now }})
	if res.Err != nil {
		return nil, res.Err
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(res.Text), &out); err != nil {
		t.Fatalf("kurs: %q", res.Text)
	}
	return out, nil
}

const yahooSiemens = `{"quotes":[
 {"exchange":"FRA","shortname":"SIEMENS AG NA","quoteType":"EQUITY","symbol":"SIE.F","exchDisp":"Frankfurt"},
 {"exchange":"GER","shortname":"SIEMENS AG NA","quoteType":"EQUITY","symbol":"SIE.DE","exchDisp":"XETRA"},
 {"exchange":"GER","shortname":"SIEMENS ENERGY AG NA","quoteType":"EQUITY","symbol":"ENR.DE","exchDisp":"XETRA"},
 {"exchange":"FRA","shortname":"Siemens Call","quoteType":"OPTION","symbol":"X1"}]}`

// 2026-11-18 16:35 UTC is 17:35 in Berlin.
const yahooSIE = `{"chart":{"result":[{"meta":{"currency":"EUR","symbol":"SIE.DE","fullExchangeName":"XETRA",
 "instrumentType":"EQUITY","regularMarketTime":1795019700,"exchangeTimezoneName":"Europe/Berlin",
 "regularMarketPrice":214.35,"chartPreviousClose":211.8,"regularMarketDayHigh":215.1,"regularMarketDayLow":210.95,
 "longName":"Siemens Aktiengesellschaft"}}],"error":null}}`

// BO-03 A share by its name, on Xetra
func TestBO03_AShareByItsNameOnXetra(t *testing.T) {
	// Given Yahoo's search finds Siemens on Xetra and in Frankfurt
	f := &yahooFake{search: yahooSiemens, charts: map[string]string{"SIE.DE": yahooSIE}}
	// When the tool is asked for "Siemens"
	out, err := runKurs(t, f, "Siemens")
	if err != nil {
		t.Fatal(err)
	}
	// Then it gives the price of the Xetra listing with its change since yesterday and the time
	if out["symbol"] != "SIE.DE" || out["kurs"] != 214.35 || out["waehrung"] != "EUR" || out["vortag"] != 211.8 ||
		out["veraenderung_prozent"] != 1.2 || out["zeit"] != "2026-11-18 17:35 CET" || out["boerse"] != "XETRA" ||
		out["tageshoch"] != 215.1 || out["tagestief"] != 210.95 || out["name"] != "Siemens Aktiengesellschaft" {
		t.Errorf("kurs %v", out)
	}
	// And the other hits as alternatives
	alt, _ := out["alternativen"].([]any)
	if len(alt) != 2 || alt[0].(map[string]any)["symbol"] != "SIE.F" || alt[1].(map[string]any)["symbol"] != "ENR.DE" {
		t.Errorf("alternativen %v", out["alternativen"])
	}
	if len(f.asked) != 2 {
		t.Errorf("asked %v", f.asked)
	}
}

// BO-04 An index by its name needs no search
func TestBO04_AnIndexByItsNameNeedsNoSearch(t *testing.T) {
	f := &yahooFake{charts: map[string]string{"^GDAXI": strings.ReplaceAll(yahooSIE, `"SIE.DE"`, `"^GDAXI"`)}}
	// When the tool is asked for "DAX"
	out, err := runKurs(t, f, "der DAX")
	if err != nil {
		t.Fatal(err)
	}
	// Then it asks Yahoo's chart for ^GDAXI only
	if len(f.asked) != 1 || f.asked[0] != "/v8/finance/chart/^GDAXI" || out["symbol"] != "^GDAXI" {
		t.Errorf("asked %v, out %v", f.asked, out)
	}
	if _, ok := out["alternativen"]; ok {
		t.Errorf("an index has alternatives: %v", out)
	}
}

// BO-05 Nothing found is said
func TestBO05_NothingFoundIsSaid(t *testing.T) {
	// Given Yahoo's search finds nothing for the name
	f := &yahooFake{search: `{"quotes":[{"symbol":"X1","quoteType":"OPTION"}]}`}
	_, err := runKurs(t, f, "Gibtsnicht AG")
	// Then the tool's error says that no security was found
	if err == nil || !strings.Contains(err.Error(), "Zu „Gibtsnicht AG“ finde ich kein Wertpapier") {
		t.Errorf("error %v", err)
	}
}

// BO-06 Yahoo refusing is the tool's error with its status
func TestBO06_YahooRefusingIsTheToolsErrorWithItsStatus(t *testing.T) {
	// Given Yahoo answers 429
	f := &yahooFake{status: 429}
	_, err := runKurs(t, f, "SAP")
	// Then the tool's error names the status and the answer's start
	if err == nil || !strings.Contains(err.Error(), "HTTP 429") || !strings.Contains(err.Error(), "Too Many Requests") {
		t.Errorf("error %v", err)
	}
}
