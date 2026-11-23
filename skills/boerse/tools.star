# The tool of the skill boerse (docs/spec/boerse.md, docs/spec/api-tools.md):
# a price from Yahoo Finance, its search and its chart.

_API = "https://query1.finance.yahoo.com/"

# The indices by their names, without a search (decision 4).
_INDEX = {
    "dax": "^GDAXI", "mdax": "^MDAXI", "tecdax": "^TECDAX", "sdax": "^SDAXI",
    "euro stoxx 50": "^STOXX50E", "eurostoxx 50": "^STOXX50E", "euro stoxx": "^STOXX50E",
    "dow jones": "^DJI", "dow": "^DJI", "s&p 500": "^GSPC", "s&p": "^GSPC", "sp500": "^GSPC",
    "nasdaq": "^IXIC", "nasdaq composite": "^IXIC",
}

# The kinds of a hit Mira gives a price of.
_ARTEN = ["EQUITY", "ETF", "INDEX", "MUTUALFUND"]

def kurs(wert):
    """Der Kurs eines Wertpapiers (Aktie, Fonds, Index) bei Yahoo Finance, bis zu 15 Minuten verzögert: Kurs, Währung, Vortag, Veränderung in Prozent, Tageshoch und -tief, die Zeit des Kurses und andere Treffer.

    Kostet zwei Abfragen bei Yahoo (die Suche und den Kurs), ein Index eine.

    Args:
      wert (str): Name, Kürzel oder ISIN, wie der Nutzer ihn sagt (SAP, Siemens Energy, DAX, DE0007164600).
    """
    w = wert.strip()
    if not w:
        fail("wert ist leer: nenne den Namen, das Kürzel oder die ISIN.")
    symbol = _INDEX.get(w.lower().replace("-", " ").removeprefix("der ").strip())
    andere = []
    if not symbol:
        symbol, andere = _suche(w)
    out = _chart(symbol)
    if andere:
        out["alternativen"] = andere
    return out

def _suche(w):
    """Die Listung nach decision 3 und bis zu vier andere Treffer."""
    j = _get("v1/finance/search", {"q": w, "quotesCount": 6, "newsCount": 0, "lang": "de-DE", "region": "DE"})
    treffer = [q for q in j.get("quotes", []) if q.get("quoteType") in _ARTEN and q.get("symbol")]
    if not treffer:
        fail("Zu „%s“ finde ich kein Wertpapier." % w)
    wahl = treffer[0]
    for q in treffer:
        if q.get("exchange") == "GER":
            wahl = q
            break
    andere = []
    for q in treffer:
        if q == wahl or len(andere) == 4:
            continue
        andere.append({"name": q.get("shortname") or q.get("longname") or q["symbol"], "symbol": q["symbol"],
                       "boerse": q.get("exchDisp", q.get("exchange", ""))})
    return wahl["symbol"], andere

def _chart(symbol):
    j = _get("v8/finance/chart/" + symbol, {"range": "1d", "interval": "1d"})
    res = (j.get("chart") or {}).get("result") or []
    m = res[0].get("meta", {}) if res else {}
    preis = m.get("regularMarketPrice")
    if preis == None:
        fail("Yahoo nennt keinen Kurs für %s." % symbol)
    vortag = m.get("chartPreviousClose") or m.get("previousClose")
    out = {
        "name": m.get("longName") or m.get("shortName") or symbol,
        "symbol": m.get("symbol", symbol),
        "boerse": m.get("fullExchangeName", ""),
        "kurs": _rund(preis),
        "waehrung": m.get("currency", ""),
        "art": m.get("instrumentType", ""),
    }
    if vortag:
        out["vortag"] = _rund(vortag)
        out["veraenderung_prozent"] = _rund((preis - vortag) / vortag * 100)
    if m.get("regularMarketDayHigh") != None:
        out["tageshoch"] = _rund(m["regularMarketDayHigh"])
    if m.get("regularMarketDayLow") != None:
        out["tagestief"] = _rund(m["regularMarketDayLow"])
    if m.get("regularMarketTime"):
        t = time.from_timestamp(int(m["regularMarketTime"]))
        zone = m.get("exchangeTimezoneName", "")
        if zone:
            t = t.in_location(zone)
        out["zeit"] = t.format("2006-01-02 15:04 MST")
    return out

def _rund(x):
    """x auf zwei Nachkommastellen."""
    return float(int(x * 100 + (0.5 if x >= 0 else -0.5))) / 100

def _get(pfad, params):
    r = api.get(_API + pfad, params = params)
    if r.ok and r.json != None:
        return r.json
    if r.status == 404:
        return {}
    if r.status == 429 or r.status >= 500:
        fail("Yahoo Finance ist gerade nicht erreichbar (HTTP %d): %s" % (r.status, r.text[:200]))
    fail("Yahoo Finance antwortet HTTP %d: %s" % (r.status, r.text[:200]))
