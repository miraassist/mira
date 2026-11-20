# The tools of the skill youtube (docs/spec/youtube.md, docs/spec/api-tools.md):
# each reads YouTube's Data API v3 and returns what the answer needs.

_API = "https://www.googleapis.com/youtube/v3/"

_KATEGORIE = {
    "1": "Film und Animation", "2": "Autos", "10": "Musik", "15": "Tiere", "17": "Sport", "19": "Reisen",
    "20": "Gaming", "22": "Leute und Blogs", "23": "Comedy", "24": "Unterhaltung", "25": "Nachrichten",
    "26": "Praktische Tipps", "27": "Bildung", "28": "Wissenschaft und Technik", "30": "Filme", "44": "Trailer",
}

_SORTIERUNG = {"relevanz": "relevance", "aufrufe": "viewCount", "neu": "date"}

def charts(land = "DE", anzahl = 5):
    """Die YouTube-Charts eines Landes. Seit Juli 2025 sind das nur Musik, Filme und Gaming, keine Trends allgemein.

    Kostet 1 Einheit des Tageskontingents.

    Args:
      land (str): Ländercode, z. B. DE, AT, CH.
      anzahl (int): Wie viele Videos, 1 bis 10.
    """
    _bereich("anzahl", anzahl, 1, 10)
    j = _get("videos", {
        "part": "snippet,statistics,contentDetails", "chart": "mostPopular", "regionCode": land.upper(),
        "maxResults": anzahl,
        "fields": "items(id,snippet(title,channelTitle,publishedAt,categoryId),statistics/viewCount,contentDetails/duration)",
    })
    return {"videos": [_video(v) for v in j.get("items", [])]}

def suche(thema, tage = 7, anzahl = 5, sortierung = "relevanz"):
    """Sucht Videos zu einem Thema, die in den letzten Tagen erschienen sind, mit Aufrufen und Länge.

    Kostet 101 Einheiten des Tageskontingents (die Suche 100, die Einzelheiten 1): pro Frage höchstens eine Suche
    je Thema.

    Args:
      thema (str): Suchbegriffe in wenigen Wörtern, in der Sprache, in der die Videos wohl sind.
      tage (int): Nur Videos der letzten so vielen Tage, 1 bis 30.
      anzahl (int): Wie viele Videos, 1 bis 10.
      sortierung (str): "relevanz", "aufrufe" (die meistgesehenen zuerst) oder "neu".
    """
    _bereich("tage", tage, 1, 30)
    _bereich("anzahl", anzahl, 1, 10)
    if sortierung not in _SORTIERUNG:
        fail("sortierung ist relevanz, aufrufe oder neu, nicht %r" % sortierung)
    j = _get("search", {
        "part": "snippet", "type": "video", "q": thema, "order": _SORTIERUNG[sortierung],
        "publishedAfter": time.now() - tage * 24 * time.hour, "maxResults": anzahl,
        "fields": "items(id/videoId)",
    })
    ids = [i["id"]["videoId"] for i in j.get("items", []) if i.get("id", {}).get("videoId")]
    if not ids:
        return {"thema": thema, "videos": [], "hinweis": "Keine Videos zu diesem Thema in den letzten %d Tagen." % tage}
    return {"thema": thema, "videos": _details(ids)}

def kanal(handle, anzahl = 5):
    """Die neuesten Videos eines Kanals, nach seinem Handle (@heise). Kostet 3 Einheiten, keine Suche.

    Args:
      handle (str): Das Handle des Kanals, mit oder ohne @.
      anzahl (int): Wie viele Videos, 1 bis 10.
    """
    _bereich("anzahl", anzahl, 1, 10)
    h = handle.strip()
    if not h.startswith("@"):
        h = "@" + h
    j = _get("channels", {"part": "snippet,contentDetails", "forHandle": h,
                          "fields": "items(id,snippet/title,contentDetails/relatedPlaylists/uploads)"})
    items = j.get("items", [])
    if not items:
        return {"handle": h, "hinweis": "Auf YouTube gibt es keinen Kanal %s." % h}
    k = items[0]
    p = _get("playlistItems", {"part": "contentDetails", "playlistId": k["contentDetails"]["relatedPlaylists"]["uploads"],
                               "maxResults": anzahl, "fields": "items(contentDetails/videoId)"})
    ids = [i["contentDetails"]["videoId"] for i in p.get("items", [])]
    out = {"kanal": k["snippet"]["title"], "handle": h, "videos": _details(ids) if ids else []}
    if not ids:
        out["hinweis"] = "Der Kanal hat noch keine Videos."
    return out

def details(video):
    """Die Einzelheiten eines Videos: Titel, Kanal, Datum, Aufrufe, Likes, Länge und der Anfang der Beschreibung.

    Kostet 1 Einheit.

    Args:
      video (str): Die Nummer des Videos (v= in der Adresse) oder die ganze Adresse.
    """
    v = video.strip()
    if "v=" in v:
        v = v.split("v=")[1].split("&")[0]
    elif "youtu.be/" in v:
        v = v.split("youtu.be/")[1].split("?")[0]
    found = _details([v], beschreibung = True)
    if not found:
        return {"hinweis": "Ein Video %s gibt es auf YouTube nicht." % v}
    return found[0]

def _details(ids, beschreibung = False):
    fields = "items(id,snippet(title,channelTitle,publishedAt,categoryId%s),statistics(viewCount,likeCount),contentDetails/duration)"
    j = _get("videos", {"part": "snippet,statistics,contentDetails", "id": ",".join(ids),
                        "fields": fields % (",description" if beschreibung else "")})
    return [_video(v, beschreibung) for v in j.get("items", [])]

def _video(v, beschreibung = False):
    s = v.get("snippet", {})
    st = v.get("statistics", {})
    out = {
        "titel": _text(s.get("title", "")),
        "kanal": s.get("channelTitle", ""),
        "datum": s.get("publishedAt", "")[:10],
        "aufrufe": int(st.get("viewCount", "0")),
        "minuten": _minuten(v.get("contentDetails", {}).get("duration", "")),
        "adresse": "https://www.youtube.com/watch?v=" + v["id"],
    }
    if s.get("categoryId"):
        out["kategorie"] = _KATEGORIE.get(s["categoryId"], s["categoryId"])
    if beschreibung:
        out["likes"] = int(st.get("likeCount", "0"))
        out["beschreibung"] = s.get("description", "")[:500]
    return out

def _get(pfad, params):
    r = api.get(_API + pfad, params = params)
    if r.ok and r.json != None:
        return r.json
    grund = ""
    if r.json != None and type(r.json) == "dict":
        e = r.json.get("error", {})
        grund = ", ".join([x.get("reason", "") for x in e.get("errors", [])]) or e.get("message", "")
    if r.status == 403 and "quotaExceeded" in grund:
        fail("YouTube lässt für heute keine Abfragen mehr zu: das Tageskontingent ist aufgebraucht.")
    if r.status >= 500:
        fail("YouTube ist gerade nicht erreichbar (HTTP %d)." % r.status)
    fail("YouTube antwortet HTTP %d: %s" % (r.status, grund or r.text[:200]))

def _bereich(name, n, lo, hi):
    if n < lo or n > hi:
        fail("%s ist %d bis %d, nicht %d" % (name, lo, hi, n))

def _minuten(d):
    """Die Länge aus ISO 8601 (PT1H2M30S) in ganzen Minuten, aufgerundet."""
    if not d.startswith("PT"):
        return 0
    sek, zahl = 0, ""
    for c in d[2:].elems():
        if c.isdigit():
            zahl += c
        elif zahl:
            sek += int(zahl) * {"H": 3600, "M": 60, "S": 1}.get(c, 0)
            zahl = ""
    return (sek + 59) // 60

def _text(t):
    for a, b in (("&amp;", "&"), ("&quot;", "\""), ("&#39;", "'"), ("&lt;", "<"), ("&gt;", ">")):
        t = t.replace(a, b)
    return t
