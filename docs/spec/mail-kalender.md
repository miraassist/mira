# Mail-Kalender – Spezifikation

Stand 2026-10-20. Beschlossen, gebaut (0.6.56). Erste Anwendung: [Pakete](pakete.md).

## Ziel

Viele Mails kündigen etwas mit einer Zeit an: ein Paket kommt morgen zwischen 10 und 12, der Zahnarzt bestätigt
Dienstag 9:30, die Bahn meldet einen Gleiswechsel, die Apotheke hat das Medikament bereit. Mira liest solche Mails,
zieht heraus, was und wann, und trägt es in einen **Kalender** ein. Dort sieht man es in der normalen Kalenderansicht,
Mira beantwortet Fragen daraus, und das Briefing liest es mit. Neuere Mails ändern den Eintrag, eine Absage löscht
ihn, Erledigtes räumt Mira weg.

Der **Kern** ist allgemein: Mail rein, isoliert auswerten, Eintrag pflegen. **Was** aus einer Mail zu holen ist und
wie man Fragen dazu beantwortet, sagt ein **Skill** je Anwendung (`pakete`, später etwa `termine`, `reisen`).

## Grundsatz

- **Nichts nachbauen, was HA anbietet:** Mails kommen über die IMAP-Integration von Home Assistant.
- **Konvention vor Einstellung:** Der Name des IMAP-Eintrags bestimmt Anwendung, Kalender und Besitzer.
- **Mails sind fremder Text:** Ausgewertet wird isoliert; heraus kommen nur geprüfte Daten, nie Anweisungen.

## 1. Der IMAP-Eintrag und sein Name

- Der Nutzer richtet in HA die **IMAP-Integration** ein. Ein Eintrag hat einen Ordner (Vorgabe: Posteingang) und eine
  **Suche** in IMAP-Syntax (etwa `OR FROM dhl.de FROM hermesworld.com`); beides lässt sich kombinieren. Mira setzt auf
  die Suche; ein eigener Ordner per Regel im Mailprogramm ist möglich, wenn die Suche nicht reicht.
- Je Anwendung ein Eintrag, denn jede Anwendung hat ihre eigenen Absender. Man kann mehrere Einträge auf dasselbe
  Postfach anlegen.
- **Der Name des Eintrags** (in HA umbenennbar, Vorgabe die Mail-Adresse) folgt der Konvention
  **`<Anwendung>`** oder **`<Anwendung> <Login>`**:
  - „Pakete“: Anwendung `pakete`, gehört dem **Haus**.
  - „Pakete stefan“: Anwendung `pakete`, gehört der Person mit dem Login-Namen `stefan` (oder die so heißt, wie beim
    [Kalender](kalender.md)).
  - Die Anwendung ist der Name eines Mail-Skills (Groß- und Kleinschreibung egal). Einträge, deren Name zu keinem
    Mail-Skill passt, liest Mira nicht.
- **Der Kalender heißt wie der Eintrag:** „Pakete“, „Pakete stefan“. Fehlt er, legt die Integration einen lokalen
  Kalender dieses Namens an und gibt ihn für Assist frei.
- Abweichende Namen per Einstellung: später, wenn jemand sie braucht.

## 2. Der Weg einer Mail

```
IMAP-Eintrag „Pakete“ (Suche)
  → HA: Ereignis imap_content (neue Mail)
  → Mira-Integration: Eintrag passt zu einem Mail-Skill? vollen Text holen (imap.fetch)
  → Agent POST /v1/mail: Name des Eintrags, Absender, Betreff, Datum, Text
  → Agent: Auswertung isoliert (Anleitung des Skills, festes Schema), Felder prüfen
  → Agent: Eintrag im Kalender anlegen, ändern oder löschen, wenn die Mail neuer ist
```

- Miras eigener HA-Benutzer darf `imap_content` nicht abonnieren (nur Admins); die **Integration** hört es. Welche
  Anwendungen es gibt, meldet der Agent in `/v1/status` (`mail_apps`); andere Einträge leitet die Integration nicht
  weiter.
- Ereignisse beim Löschen einer Mail (`initial: false`) ignoriert sie.
- Der Agent bearbeitet die Mails eines Kalenders nacheinander.
- HTML-Mails macht der Agent vor der Auswertung zu Text.
- **Verpasste Mails** (kommen bei Abfrage statt Push zwei Mails auf einmal, meldet HA vermutlich nur die letzte):
  zunächst nicht nachgeholt; im Test beobachten.

## 3. Auswertung: isoliert, festes Schema

- Ein **isolierter** Modellaufruf: kein Haus, keine Werkzeuge, kein Gedächtnis, kein Verlauf. Er bekommt die
  **Mail-Anleitung des Skills** und die Mail, als Daten gekennzeichnet.
- **Modell:** Rolle `mail` unter `routing`; ohne Angabe das Modell für Hintergrundaufgaben. Ein Cloud-Modell sieht
  die Mails (Namen, Adressen, Bestellungen, bei Terminen auch Gesundheitsdaten); der Guide rät zu einem lokalen.
- **Schema** (JSON):

  | Feld | Bedeutung |
  |---|---|
  | `relevant` | ob die Mail etwas für diesen Kalender ankündigt; sonst wird sie verworfen |
  | `title` | Titel des Eintrags, kurz („Paket von Amazon: Druckerpatronen“) |
  | `keys` | alle Kennungen der Sache (Sendungs-, Bestell-, Buchungsnummer); leer, wenn keine |
  | `status` | `scheduled` (angekündigt, bestätigt, geändert), `cancelled` (abgesagt), `done` (erledigt, zugestellt) |
  | `day` | Tag (YYYY-MM-DD); leer, wenn die Mail keinen nennt |
  | `start`, `end` | Uhrzeit oder Zeitfenster (HH:MM), leer für ganztägig |
  | `location` | Ort, kurz |
  | `details` | Einzelheiten, je Zeile „Bezeichnung: Wert“; nie Codes, PINs oder Passwörter |

- **Prüfung im Code**, bevor etwas übernommen wird: `day` gültig und höchstens 7 Tage vor, höchstens ein Jahr nach dem
  Datum der Mail; Uhrzeiten gültig, `end` nach `start` (sonst ohne `end`); jeder Schlüssel nur Buchstaben, Ziffern
  und Bindestrich (höchstens 40), höchstens 8; Titel (80) und Ort (100) gekürzt, Einzelheiten höchstens 6 Zeilen zu
  100 Zeichen. Was nicht passt, fällt weg.
- **Ohne Titel oder Tag gibt es keinen Eintrag**, bis eine Mail den Tag nennt (eine Versandmail ohne Zustelltag
  trägt nichts ein). Eine Absage braucht keinen Tag: Sie löscht den Eintrag, den sie über einen Schlüssel findet.

## 4. Der Kalendereintrag

- Mit `start` (und `end`): Termin; sonst ganztägig am `day`. Ohne `end` dauert ein Termin eine Stunde.
- **Beschreibung:** die Einzelheiten, darunter feste Zeilen, die Mira liest:
  - `mira-mail: <key>, <key>` (die Kennungen; ohne Schlüssel `mira-mail: -`),
  - `Stand: <Datum der Mail>` und `Status: <status>`.
- **Kennungen:** Verschiedene Mails zur selben Sache nennen oft verschiedene Nummern (der Shop die Bestellnummer, der
  Paketdienst die Sendungsnummer). Vor dem Anlegen sucht Mira in den Einträgen des Kalenders von 30 Tagen vor bis ein
  Jahr nach heute einen, der **einen** Schlüssel mit der Mail teilt (Groß- und Kleinschreibung egal). Gefunden:
  ändern, und die Schlüssel beider kommen zusammen. Die interne Kennung des Kalenders hilft nicht, die vergibt der
  Kalender.
- **Ohne Schlüssel:** Ein zweiter isolierter Aufruf bekommt die offenen Einträge (Titel, Tag) und sagt, ob die Mail
  denselben betrifft (Nummer) oder einen neuen. Im Zweifel neu.
- **Was die neue Mail nicht nennt, bleibt:** die Einzelheiten, deren Bezeichnung sie nicht nennt; der Ort, wenn sie
  keinen nennt; das Zeitfenster, wenn sie denselben Tag ohne Zeit nennt; der Titel, wenn der neue nur sein Anfang ist
  („Paket von Amazon“ lässt „Paket von Amazon: Druckerpatronen“ stehen).
- **Die neueste Mail gewinnt:** Ist das Datum der Mail älter als der `Stand` des gefundenen Eintrags, wird sie
  verworfen. So ist es gleich, in welcher Reihenfolge Mails ankommen; doppelte Verarbeitung ändert nichts.
- **Status:** `cancelled` löscht den Eintrag; `done` lässt ihn bis zum Ende seines Tages stehen.
- **Aufräumen** (stündlich): Einträge mit Status `done`, deren Tag vorbei ist, und alle Einträge, deren Tag mehr als
  eine Woche vorbei ist, löscht Mira. Nur in Mail-Kalendern und nur Einträge mit `mira-mail:`.

## 5. Mail-Skills

Ein Skill wird zur Mail-Anwendung durch einen Abschnitt `mail` im Kopf:

```yaml
metadata:
  mira:
    mail:
      extract: >-
        Anleitung für die Auswertung: was eine relevante Mail ist, wie Titel und Kennung zu bilden sind …
```

- `extract` geht als Anleitung an den isolierten Aufruf. Das Schema und die Prüfung legt der Kern fest, nicht der
  Skill.
- Der Text des Skills ist, wie bei jedem Skill, die Anleitung für Fragen („Wann kommt mein Paket?“).
- Mail-Skills ohne Pflichteinstellungen schalten sich ein, wenn ihr Ordner da ist, wie die eingebauten Ziele: Der
  Name des IMAP-Eintrags ist der Schalter.

## 6. Mitlesen im Briefing

Das persönliche Briefing bekommt `{{person_login}}` und liest den Kalender „Pakete {{person_login}}“ mit, wenn es ihn
gibt; das Haus-Briefing „Pakete“, wenn er in seiner Einstellung `kalender` steht.

## 7. Test

- Die Test-HA bekommt einen IMAP-Server im Container (GreenMail) und einen IMAP-Eintrag „Pakete“;
  `go run ./cmd/testha mail` stellt die Beispielmails zu (testenv/README.md).
- Beispielmails: selbst geschriebene nach den Mustern von DHL, GLS, Hermes und eBay (im Repo, ohne echte Daten);
  echte Mails nur lokal (`testenv/mails/`, nicht im Git).
- Geprüft wird: Eintrag angelegt; neuere Mail ändert ihn; ältere wird verworfen; Absage löscht; Abholcode erscheint
  nicht; Werbemail wird verworfen.
