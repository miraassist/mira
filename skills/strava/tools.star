# The tools of the skill strava (docs/spec/strava.md, docs/spec/api-tools.md):
# each reads Strava's web API and returns what the answer needs, in
# kilometres, metres of climbing and minutes.

_API = "https://www.strava.com/api/v3/"

_WOCHENTAG = {"Mon": 0, "Tue": 1, "Wed": 2, "Thu": 3, "Fri": 4, "Sat": 5, "Sun": 6}

def letzte_tour():
    """Die neueste Aktivität auf Strava: Sportart, wann, wie weit, wie lange, Höhenmeter und Schnitt.
    """
    liste = _get("athlete/activities", {"per_page": 1})
    if not liste:
        return {"hinweis": "Auf Strava ist noch keine Aktivität."}
    return _aktivitaet(liste[0])

def zeitraum(zeitraum = "woche"):
    """Was in dieser Woche (ab Montag) oder diesem Monat zusammenkam, je Sportart: Anzahl, Kilometer, Höhenmeter, Zeit.

    Gezählt werden die letzten 100 Aktivitäten; reichen sie nicht bis zum Anfang des Zeitraums, steht das in
    nur_letzte_100.

    Args:
      zeitraum (str): "woche" oder "monat".
    """
    if zeitraum not in ("woche", "monat"):
        fail("zeitraum ist \"woche\" oder \"monat\", nicht %r" % zeitraum)
    heute = time.now()
    if zeitraum == "woche":
        ab = _montag(heute)
    else:
        ab = heute.format("2006-01") + "-01"
    liste = _get("athlete/activities", {"per_page": 100})
    drin = [a for a in liste if a["start_date_local"][:10] >= ab]
    summe = {}
    for a in drin:
        s = summe.setdefault(a["sport_type"], {"anzahl": 0, "km": 0.0, "hoehenmeter": 0.0, "minuten": 0})
        s["anzahl"] += 1
        s["km"] += a["distance"] / 1000
        s["hoehenmeter"] += a["total_elevation_gain"]
        s["minuten"] += a["moving_time"] // 60
    for s in summe.values():
        s["km"] = _runde(s["km"])
        s["hoehenmeter"] = int(math.round(s["hoehenmeter"]))
    out = {"zeitraum": zeitraum, "ab": ab, "je_sportart": summe}
    if not drin:
        out["hinweis"] = "In diesem Zeitraum ist auf Strava keine Aktivität."
    if len(liste) == 100 and liste[-1]["start_date_local"][:10] >= ab:
        out["nur_letzte_100"] = True
    return out

def serie():
    """Die Serie: wie viele Wochen (Montag bis Sonntag) in Folge mit mindestens einer Aktivität, bis zu dieser.

    Hat diese Woche noch keine, zählt die Serie ab letzter Woche.
    """
    liste = _get("athlete/activities", {"per_page": 100})
    if not liste:
        return {"wochen": 0, "hinweis": "Auf Strava ist noch keine Aktivität."}
    wochen = {_montag(time.parse_time(a["start_date_local"])): True for a in liste}
    woche = _montag(time.now())
    if woche not in wochen:
        woche = _vorwoche(woche)
    n = 0
    for _ in range(100):
        if woche not in wochen:
            break
        n += 1
        woche = _vorwoche(woche)
    return {"wochen": n}

def ausruestung():
    """Die Räder und Schuhe auf Strava mit ihren Kilometern. Einzelteile wie die Kette kennt Strava nicht.
    """
    a = _get("athlete")
    teil = lambda x: {"name": x.get("name", ""), "km": _runde(x.get("distance", 0) / 1000)}
    out = {"raeder": [teil(x) for x in (a.get("bikes") or [])], "schuhe": [teil(x) for x in (a.get("shoes") or [])],
           "einzelteile": "Strava kennt keine Einzelteile wie Kette oder Reifen, nur die Kilometer eines Rads."}
    if not out["raeder"]:
        out["hinweis"] = "Auf Strava ist kein Rad eingetragen."
    return out

def bestleistungen():
    """Die längste Fahrt und der größte Anstieg unter den letzten 100 Aktivitäten, dazu die Summen dieses Jahres und insgesamt.

    Die Summen sind keine Bestleistung.
    """
    liste = _get("athlete/activities", {"per_page": 100})
    out = {}
    if liste:
        weit = sorted(liste, key = lambda a: a["distance"])[-1]
        hoch = sorted(liste, key = lambda a: a["total_elevation_gain"])[-1]
        out["laengste"] = _aktivitaet(weit)
        out["groesster_anstieg"] = _aktivitaet(hoch)
    else:
        out["hinweis"] = "Auf Strava ist noch keine Aktivität."
    ich = _get("athlete")
    stats = _get("athletes/%d/stats" % ich["id"])
    for name in ("ytd_ride_totals", "ytd_run_totals", "all_ride_totals", "all_run_totals"):
        t = stats.get(name) or {}
        out[name] = {"anzahl": t.get("count", 0), "km": _runde(t.get("distance", 0) / 1000),
                     "hoehenmeter": int(math.round(t.get("elevation_gain", 0)))}
    return out

def _get(pfad, params = None):
    r = api.get(_API + pfad, params = params)
    if r.status == 401 and "activity:read_permission" in r.text:
        fail("Strava erlaubt die Aktivitäten nicht: Strava neu verbinden und dabei Aktivitäten angehakt lassen.")
    if r.status == 429:
        fail("Strava lässt gerade keine Anfragen mehr zu; in einer Viertelstunde wieder.")
    if r.status >= 500:
        fail("Strava ist gerade nicht erreichbar (HTTP %d)." % r.status)
    if not r.ok or r.json == None:
        fail("Strava antwortet HTTP %d: %s" % (r.status, r.text[:200]))
    return r.json

def _aktivitaet(a):
    return {
        "sportart": a["sport_type"],
        "name": a.get("name", ""),
        "start": a["start_date_local"][:16].replace("T", " "),
        "km": _runde(a["distance"] / 1000),
        "minuten": a["moving_time"] // 60,
        "hoehenmeter": int(math.round(a["total_elevation_gain"])),
        "schnitt_kmh": _runde(a.get("average_speed", 0) * 3.6),
    }

def _runde(x):
    return math.round(x * 10) / 10

def _montag(t):
    """Der Montag der Woche von t, als 2026-11-16."""
    return (t - _WOCHENTAG[t.format("Mon")] * 24 * time.hour).format("2006-01-02")

def _vorwoche(montag):
    return (time.parse_time(montag + "T12:00:00Z") - 7 * 24 * time.hour).format("2006-01-02")
