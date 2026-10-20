# Mira – Anleitung

Mira ist die Sprachassistentin, wie sie immer hätte sein sollen. Du sprichst mit ihr wie mit einem Menschen: Sie versteht, was du meinst, fragt nach, wenn etwas fehlt, und bleibt beim Thema, auch über mehrere Sätze. Sie merkt sich, was dir wichtig ist, schaut im Internet nach, kennt deinen Müllkalender – und sie passt von sich aus auf dein Zuhause auf und meldet sich, wenn etwas ist.

Home Assistant ist dabei ihr Werkzeugkasten: Lampen, Sensoren, Sprach-Satelliten. Dass Mira das Licht einschalten kann, ist selbstverständlich. Was sie ausmacht, steht hier vorne.

## Was Mira anders macht

- **Echte Gespräche.** Keine festen Kommandos. Mira fragt zurück, statt zu raten, und weiß im nächsten Satz noch, worum es ging.
- **Sie passt auf.** Sag ihr einmal „Achte auf den Wasserverbrauch“ – ab dann behält sie das im Blick und meldet sich, wenn etwas ungewöhnlich ist.
- **Sie meldet sich im richtigen Moment.** Im Raum, in dem du gerade bist, nicht mitten in der Nacht, und Dringendes sofort.
- **Sie merkt sich Dinge** – fürs ganze Haus und für jeden persönlich.
- **Sie kann mehr als das Haus.** Skills wie der Abfuhrkalender, eine Websuche, schwierige Fragen an ein stärkeres Modell.

## Ein Gespräch mit Mira

▶ [Eine Aufgabe einrichten: Mira fragt nach, schlägt vor und richtet sie nach deinem Ja ein](https://mira-assist.com/videos/mira-chat-de.mp4#t=266,312)

So klingt das, zum Beispiel beim Einrichten einer Aufgabe:

> **Du:** Sag Bescheid, wenn die Nachttischlampe zu einer ungewöhnlichen Zeit angeht.
>
> **Mira:** Welche Uhrzeit ist für dich ungewöhnlich, etwa zwischen Mitternacht und 6 Uhr?
>
> **Du:** Zwischen eins und vier.
>
> **Mira:** Mein Vorschlag: Wenn die Nachttischlampe zwischen 1 und 4 Uhr angeht, sage ich dir Bescheid. Soll ich das so einrichten?
>
> **Du:** Ja.
>
> **Mira:** Alles klar, die Aufgabe ist eingerichtet.

Und bei einer Frage, die das Haus gar nicht betrifft:

> **Du:** Was wird nächste Woche abgeholt?
>
> **Mira:** Nächste Woche werden am Montag der Wertstoff, am Dienstag der Restabfall und am Donnerstag der Bioabfall abgeholt.

## So sprichst du mit Mira

- **Per Sprache**: über einen Sprach-Satelliten (z. B. den Echo im Wohnzimmer). Sag das Aktivierungswort und dann, was du möchtest. Mira weiß, in welchem Raum der Satellit steht: „Mach das Licht an“ meint das Licht in diesem Raum. Ist ihre Antwort eine Frage, hört der Satellit gleich weiter zu.
- **Per Telegram**: schreib dem Bot, den du in Home Assistant eingerichtet hast (Integration „Telegram bot“). Damit Mira antwortet, muss dein Chat bei deiner Person als Kanal eingetragen sein (siehe unten). Antworten dürfen hier länger sein, mit Listen und Links. `/neu` beginnt ein neues Gespräch.
- **In der Home-Assistant-App**: Der Assist-Chat der Companion App spricht ebenfalls mit Mira, getippt oder gesprochen.

Mira antwortet in der Sprache, in der du sie ansprichst.

## Mira passt aufs Haus auf: Aufgaben

Gib Mira eine dauerhafte Aufgabe, und sie behält etwas für dich im Blick:

- „Achte auf den Gasverbrauch und sag Bescheid, wenn in einer Stunde ungewöhnlich viel verbraucht wird.“
- „Sag Bescheid, wenn nachts ungewöhnlich viel Strom verbraucht wird.“
- „Melde dich sofort, wenn einer der Lecksensoren anschlägt.“

▶ [Ein Auftrag für den Lecksensor und die Meldung, wenn er anschlägt](https://mira-assist.com/videos/mira-chat-de.mp4#t=623,673)

So läuft das Einrichten ab:

1. Fehlt Mira etwas, fragt sie zurück, statt zu raten – etwa „Welche Uhrzeit ist für dich ungewöhnlich?“. Antworte einfach; das Gespräch bleibt beim Thema, auch über mehrere Sätze.
2. Dann plant sie. Geht das schnell, nennt sie dir den Vorschlag gleich; sonst sagt sie „Ich plane das“ und meldet sich, sobald sie fertig gesprochen hat – im selben Raum oder auf Telegram.
3. Der Vorschlag sagt, was sie beobachtet, und nennt die Bedingung: „Wenn die Nachttischlampe angeht, sage ich dir Bescheid. Bedingung: nur zwischen 1 und 4 Uhr. Soll ich so vorgehen?“
4. Sag **„Ja“**, dann richtet sie die Aufgabe ein. Mit „Nein, lass es“ bricht sie ab. Antwortet niemand, verfällt der Vorschlag nach einer Viertelstunde.

Beim Beurteilen vergleicht Mira zum Beispiel den Verbrauch der letzten Stunde mit derselben Stunde der vergangenen zwei Wochen, weiß, wer zu Hause ist, kennt, was du ihr gemerkt hast, und erinnert sich, was sie zu dieser Aufgabe schon gesagt hat. Dieselbe Sache meldet sie nicht ständig wieder, nur wenn es schlimmer wird.

- „Welche Aufgaben hast du?“ – „Pausier die Gas-Aufgabe.“ – „Lösch die Aufgabe mit dem Strom.“ – „Mach einen Probelauf der Gas-Aufgabe.“
- In Home Assistant unter Mira → **Aufgaben** siehst du alle Aufgaben mit an/aus, Probelauf und dem Protokoll ihrer Urteile, dazu Vorschläge, die noch auf dein Ja warten.

![Mira im Seitenmenü von Home Assistant: eine pausierte und zwei laufende Aufgaben](img/aufgaben-liste-de.png)

Unter der Haube ist jede Aufgabe eine ganz normale **Automation in Home Assistant** mit dem Label „Mira“: Home Assistant entscheidet, *wann* etwas passiert (stündlich, wenn ein Sensor auslöst, nur nachts …), Mira entscheidet, *was* davon dich interessiert. „In Home Assistant bearbeiten“ öffnet sie im Editor; dort kannst du sie auch selbst ändern.

![Die Gas-Aufgabe im Automations-Editor von Home Assistant: stündlicher Auslöser, Aktion „Mira: Bewerten“](img/ha-automation-de.png)

Eigene Automationen können Mira genauso nutzen: die Aktionen **Mira: Bewerten** (`mira.assess`), **Mira: Entscheiden** (`mira.decide`, eine Ja/Nein-Antwort für Bedingungen) und **Mira: Nachricht senden** (`mira.send_message`). Aufgaben gehören vorerst dem ganzen Haus und nutzen beim Beurteilen das Haus-Gedächtnis.

## Mira meldet sich von selbst

Mira meldet sich, wenn etwas passiert: ein Lecksensor schlägt an, die Waschmaschine ist fertig, morgen wird der Müll abgeholt, eine Aufgabe findet etwas Ungewöhnliches. Wie sie dir das sagt, hängt vom Raum und von der Wichtigkeit ab.

▶ [Eine Nachricht fürs Haus hinterlassen und später nach Benachrichtigungen fragen](https://mira-assist.com/videos/mira-chat-de.mp4#t=195,227)

Pro Satellit stellst du ein:

- **Anzeige**: Ein LED-Ring (z. B. des Echo) pulsiert in Türkis, solange etwas wartet – ganz ohne Bewegungsmelder. Ein Satellit mit Bildschirm wie die Echo Show blendet stattdessen „Mira · 1 Nachricht wartet“ ein, sobald etwas Neues kommt, und nimmt es weg, wenn alles gehört ist; das erkennt Mira von selbst. Frag dann: **„Hast du eine Benachrichtigung?“** oder „Gibt es was Neues?“.
- **Beim Hereinkommen ansagen**: Merkt ein Bewegungsmelder, dass jemand den Raum betritt, sagt Mira von selbst an, was ansteht; die erste Bewegung nach der Ruhezeit beginnt mit „Guten Morgen“. Ohne Bewegungsmelder oder ausgeschaltet spricht sie dich nicht von sich aus an.

Mehrere Nachrichten fasst Mira zu einer Ansage zusammen, das Wichtigste zuerst und mit Uhrzeit, wo sie hilft („heute Nacht um drei …“).

**Nebenbei**: Wenn du ohnehin mit dem Satelliten sprichst, hängt Mira Wartendes an ihre Antwort an – „Das Licht ist an. Übrigens: Morgen wird der Wertstoff abgeholt.“ Danach gilt die Nachricht als gehört, und die Anzeige geht aus. Fragst du nach („Was genau?“), bekommst du die Details.

**Wichtigkeit**:

| Stufe | Bedeutung | Was passiert |
|---|---|---|
| 1 | nebenbei | wartet auf die nächste Gelegenheit |
| 2 | heute wichtig | geht morgens nach 9 Uhr privat raus, wenn bis dahin niemand sie gehört hat |
| 3 | dringend | sofort, auch nachts: angesagt auf allen Satelliten und privat an alle |

Wenn Mira von sich aus spricht, leuchtet der LED-Ring des Raums ruhig, solange sie spricht.

**Ruhezeit**: In der Ruhezeit (z. B. 23:30–6:30) sagt Mira nichts an – außer Dringendem und deinen Erinnerungen. LED-Ringe leuchten trotzdem; was nachts kommt, siehst du morgens.

**Ruhemodus**: Der Schalter „Ruhemodus“ in Home Assistant stellt alle Ansagen ab, bis du ihn wieder ausschaltest.

**Vertrauliches und Persönliches**: Nachrichten können für eine Person bestimmt oder vertraulich sein. Vertrauliches liest Mira nur vor, wenn die Einstellung es erlaubt (z. B. nur wenn die Person allein zu Hause ist); sonst kommt es privat über die Kanäle der Person (Telegram, Companion-App).

![Nachrichten: wartende und zugestellte](img/meldungen-de.png)

## Mira merkt sich Dinge

- „Merk dir, dass der Schornsteinfeger im März kommt.“
- „Merk dir mal was.“ – Mira fragt „Was denn?“, und du sagst es im nächsten Satz.
- „Was weißt du über den Schornsteinfeger?“ – „Vergiss das mit dem Schornsteinfeger.“

▶ [Mira etwas merken lassen, auch mit „Merk dir mal was“](https://mira-assist.com/videos/mira-chat-de.mp4#t=416,467)

Was Mira weiß, nutzt sie auch beim Aufpassen: Sag ihr „Merk dir, die Nachttischlampe nach Mitternacht ist ungewöhnlich“, und ihre Aufgaben beurteilen danach.

Es gibt ein Gedächtnis fürs **Haus** und eins für **jede Person**:

- Weiß Mira, wer spricht, merkt sie sich es für diese Person. Ins Haus-Gedächtnis kommt es nur, wenn du es ausdrücklich sagst („merk dir fürs Haus …“).
- Mira weiß, wer spricht: über Telegram, im Chat der HA-App (über deinen HA-Benutzer) und am Satelliten nur, wenn du allein zu Hause bist und das bei deiner Person eingeschaltet hast („Allein zu Hause sind die Satelliten ich“). Sonst ist es am Satelliten das Haus.
- Dein persönliches Gedächtnis sieht nur Mira im Gespräch mit dir. Das Haus-Gedächtnis gilt für alle.
- In fremde Gedächtnisse schreibt niemand.

Nachts fasst Mira jedes Gedächtnis zu einem kurzen Profil zusammen. In Home Assistant siehst du unter Mira → **Gedächtnis** das des Hauses und dein eigenes, mit wer es wann gesagt hat, und kannst Fakten bearbeiten oder vergessen. Die Gedächtnisse der anderen sieht dort niemand, auch kein Admin.

![Annas Sicht: das Gedächtnis des Hauses und ihr eigenes](img/gedaechtnis-de.png)

## Skills: zum Beispiel die Müllabfuhr

Skills sind Zusatzfähigkeiten, die die Einrichtung freischaltet. Mira öffnet einen Skill nur, wenn eine Frage ihn braucht.

**Abfall**: Mira kennt den Abfuhrkalender deines Entsorgers. Er ist ein Kalender in Home Assistant (zum Beispiel über die Integration „Remote Calendar“ mit dem iCal-Link des Entsorgers); in `mira.yaml` steht beim Skill `abfall` sein Name.

- „Wann wird der Restmüll abgeholt?“ – „Was wird nächste Woche abgeholt?“
- Am Vorabend erinnert Mira von selbst: „Morgen wird der Wertstoff abgeholt.“
- Ist die Abfuhr heute, sagt sie es, falls die Tonne noch raus muss.

**Briefing**: eine kurze Übersicht, was ansteht, in drei, vier Sätzen.

- Morgens stellt Mira zusammen, was heute ist: Wetter, Termine, Müllabfuhr und, wenn du Themen angibst, zwei Schlagzeilen. Sie sagt es an, wenn das erste Mal jemand in einen Raum mit „beim Hereinkommen ansagen“ kommt, sonst leuchtet die Anzeige oder sie erwähnt es nebenbei. Abends kommt dasselbe für morgen, aber nur, wenn morgen etwas ansteht.
- **Dein eigenes Briefing:** Erreicht Mira dich persönlich (dein Handy oder Telegram unter „Konfigurieren → Personen“, oder „allein zu Hause bin ich“), bekommst du morgens zusätzlich eine eigene Nachricht. Darin stehen deine Termine von heute, wenn du einen eigenen Kalender hast (einer, der so heißt wie dein Login-Name in Home Assistant oder wie du als Person; bei Login `stefan` also „Stefan“), und zwei Schlagzeilen zu deinen Themen, wenn Mira sie kennt: „Merk dir, mich interessieren der KSC und E-Autos.“ Hast du weder Termine noch Themen, kommt nichts.
- „Was steht heute an?“ – „Was ist morgen los?“ – „Gib mir mein Briefing.“ Auf Nachfrage nimmt Mira auch deinen eigenen Kalender dazu, wenn sie weiß, wer spricht.
- In `mira.yaml` stehen beim Skill `briefing` die Kalender fürs Haus (`kalender`) und die Nachrichten-Themen (`themen`). Ohne `kalender` nimmt das Haus-Briefing alle freigegebenen Kalender, auch persönliche, und das ganze Haus hört deren Termine.

**Pakete**: Mira liest die Versandmails der Paketdienste und weiß, wann welches Paket kommt.

- „Wann kommt mein Paket?“ – „Kommt heute was?“ – „Ist das Paket von Amazon schon da?“
- Im Briefing: „Heute kommt zwischen 10 und 12 dein Paket von Amazon.“
- Eine neuere Mail ändert den Eintrag (neuer Tag, Zeitfenster, abholbereit), eine Stornierung löscht ihn, Zugestelltes räumt Mira am nächsten Tag weg. Abholcodes nennt Mira nie.

So richtest du es ein:

1. In Home Assistant die Integration **IMAP** hinzufügen, mit deinem Mail-Postfach.
2. Beim Einrichten als **Suche** nur die Paketdienste angeben, zum Beispiel `OR OR FROM dhl.de FROM hermesworld.com FROM gls-group.eu`.
3. Nach dem Einrichten zeigt Home Assistant das neue Gerät („IMAP (deine Adresse)“). Das **Gerät** umbenennen in **„Pakete“** (für alle im Haus) oder in **„Pakete“ und deinen Login-Namen**, etwa „Pakete stefan“ (nur für dich).

Den Kalender „Pakete“ (oder „Pakete stefan“) legt Mira selbst an; dort siehst du die Pakete auch in der Kalenderansicht. Mira liest nur Mails, die ab jetzt ankommen.

Gut zu wissen: Das Modell, das die Mails liest, sieht deren Inhalt (Namen, Adresse, Bestellungen). Mit einem lokalen Modell bleibt das im Haus; in `mira.yaml` steht es unter `routing` als `mail`. Gmail braucht ein App-Passwort; Outlook.com und Microsoft 365 gehen mit der IMAP-Integration nicht.

▶ [Die Müllabfuhr: „Wann wird der Restmüll abgeholt?“ und „Was wird nächste Woche abgeholt?“](https://mira-assist.com/videos/mira-chat-de.mp4#t=592,623)

## Eigenen Skill erstellen

Ein Skill ist ein Ordner mit einer Datei `SKILL.md`. Darin steht eine Anleitung für Mira, auf Wunsch mit Einstellungen und Zeitplänen. Ein Skill führt keinen Programmcode aus; er sagt Mira nur, was sie wann und womit tun soll, mit den Werkzeugen, die sie ohnehin hat (Kalender lesen, Nachrichten, Haussteuerung und so weiter).

**1. Ordner anlegen.** Die Skills liegen im Skill-Verzeichnis, standardmäßig `skills/` neben Mira. Dort legst du einen Ordner an. Sein Name ist der Name des Skills: kleine Wörter mit Bindestrichen, zum Beispiel `blumen`. Läuft Mira im Container, bindest du einen eigenen Ordner als Volume ein und trägst ihn zusätzlich unter `skills.dirs` in der `mira.yaml` ein. Die mitgelieferten Skills bleiben dabei erhalten.

**2. `SKILL.md` schreiben.** Die Datei beginnt mit einem Kopfbereich zwischen zwei Zeilen `---`, danach folgt die Anleitung als Text:

- `name`: genau der Name des Ordners.
- `description`: ein Satz, wann der Skill gebraucht wird. Mehr sieht Mira nicht, bis sie den Skill öffnet; ist der Satz gut, öffnet sie ihn zur rechten Zeit.
- Optional unter `metadata:` → `mira:`:
  - `settings`: Angaben, die jeder Haushalt selbst einträgt, jeweils mit einer `description` (die erscheint auch in Fehlermeldungen) und `required: true`, wenn der Skill ohne sie nicht arbeiten kann.
  - `schedules`: Aufgaben nach Zeitplan. `cron` ist die Zeit (fünf Felder, zum Beispiel `0 10 * * 0` für sonntags um 10 Uhr), `task` ist, was Mira dann tun soll. Das Ergebnis ist entweder nichts (`importance: 0`, `spoken` leer) oder eine kurze Nachricht an das Haus. Mit `per_person: true` läuft die Aufgabe einmal für jede Person, die Mira persönlich erreicht (eigener Kanal oder „allein zu Hause bin ich“). `{{person}}` und `{{person_calendar}}` werden eingesetzt (der Kalender, der so heißt wie der Login-Name oder die Person; „-“ ohne), Mira gibt dazu, was sie über die Person weiß, und die Nachricht geht an diese Person statt ans Haus.
- Der Text unter dem Kopfbereich ist die Anleitung für Mira, in der Du-Form. Wo `{{name}}` steht, setzt Mira die Einstellung `name` ein.

**3. Einschalten.** Ein Skill läuft erst, wenn er in der `mira.yaml` unter `skills.enabled` steht, mit seinen Einstellungen darunter. Hat er keine, genügt der Name mit einem Doppelpunkt. Nur die mitgelieferten Gesprächs-Skills für Aufgaben, Merken, Termine, To-dos und Wecker schalten sich selbst ein.

**Beispiel: Blumen gießen.** Der Ordner `skills/blumen` enthält die Datei `SKILL.md`:

```markdown
---
name: blumen
description: Welche Pflanzen wann gegossen werden; erinnert sonntags ans Gießen.
metadata:
  mira:
    settings:
      pflanzen:
        description: Die Pflanzen mit ihrem Rhythmus, zum Beispiel "Farn jeden Sonntag, Kaktus am ersten Sonntag im Monat".
        required: true
    schedules:
      - cron: "0 10 * * 0"
        task: >-
          Heute ist Sonntag. Welche dieser Pflanzen müssen heute gegossen werden: {{pflanzen}}?
          Ist keine dran: importance 0 und spoken leer. Sonst: importance 1, und spoken
          nennt kurz, was gegossen werden muss.
---
# Blumen gießen

Die Pflanzen dieses Haushalts und ihr Rhythmus: {{pflanzen}}

Fragt jemand, wann etwas gegossen werden muss, antworte aus dieser Liste und
rate nicht. Fehlt eine Pflanze in der Liste, sag das ehrlich.
```

In der `mira.yaml` wird der Skill eingeschaltet:

```yaml
skills:
  enabled:
    blumen:
      pflanzen: "Farn jeden Sonntag, Kaktus am ersten Sonntag im Monat"
```

Danach Mira neu starten. „Wann muss der Kaktus gegossen werden?“ beantwortet sie aus der Liste, und sonntags um 10 Uhr meldet sie sich von selbst. Den Zeitplan probierst du mit `mira skill blumen` sofort aus.

**Wenn etwas nicht stimmt:** Mira startet trotzdem, nur der betroffene Skill läuft nicht. Im Log steht `skill not started` mit einem Satz, was falsch ist, zum Beispiel „Die Einstellung „pflanzen“ fehlt in der mira.yaml unter skills.enabled“. Dieselbe Meldung zeigt Home Assistant unter Einstellungen → System → **Reparaturen**. Nach der Korrektur startest du Mira neu; die Meldung verschwindet, sobald der Skill läuft.

## Kalender und Listen

Mira nutzt die Kalender und To-do-Listen aus Home Assistant, die für Assist freigegeben sind (Einstellungen → Sprachassistenten → Entitäten freigeben). Mehrere Kalender sind kein Problem.

- „Was steht diese Woche an?“ – „Wann ist der Elternabend?“ Mira liest die Termine knapp, nach Tag; lange Beschreibungen nennt sie nur, wenn du danach fragst.
- „Trag mir Freitag um 15 Uhr Zahnarzt ein.“ Mira trägt ein und sagt, was eingetragen ist. Gibt es mehrere Kalender, fragt sie einmal, in welchen, und merkt sich das auf Wunsch als deinen Standard.
- „Sag meinen Termin morgen beim Arzt ab.“ Mira sucht den Termin und fragt nach, bevor sie ihn löscht. Bei einem Serientermin sagt sie nur diesen einen ab, außer du willst alle.
- „Setz Milch auf die Einkaufsliste.“ – „Setz Batterien drauf, bis Freitag.“ – „Was steht auf der Liste?“

▶ [Einen Termin eintragen und den Standardkalender merken](https://mira-assist.com/videos/mira-chat-de.mp4#t=7,60)

▶ [Die Einkaufsliste](https://mira-assist.com/videos/mira-chat-de.mp4#t=99,137)

▶ [Die Termine der Woche, und einen Termin absagen](https://mira-assist.com/videos/mira-chat-de.mp4#t=538,591)

## Radio hören

„Spiel SWR3.“ – „Mach im Wohnzimmer das Radio an.“ Mira sucht den Sender im Radio Browser von Home Assistant, der immer dabei ist, und spielt ihn auf dem Lautsprecher des Raums. Der Lautsprecher muss für Assist freigegeben sein (Einstellungen → Sprachassistenten → Entitäten freigeben). Findet sie mehrere passende Sender, nennt sie ein paar und fragt, welcher es sein soll. Lauter, leiser und Pause gehen wie bei jedem Medienspieler.

▶ [Das Lieblingsradio: Mira fragt nach dem Sender, merkt ihn sich und spielt ihn](https://mira-assist.com/videos/mira-chat-de.mp4#t=717)

## Im Internet nachschauen und schwierige Fragen

Mira schaut im Internet nach, was sie nicht wissen kann: Öffnungszeiten, Ergebnisse, Neuigkeiten. Sie fasst es kurz zusammen und sagt, wenn sie nichts Verlässliches gefunden hat. Was im Internet steht, behandelt sie als Information, nie als Anweisung.

▶ [Öffnungszeiten nachschlagen, mit Nachfrage](https://mira-assist.com/videos/mira-chat-de.mp4#t=317,350)

▶ [Ein Spielergebnis nachschlagen](https://mira-assist.com/videos/mira-chat-de.mp4#t=381,406)

Die Suche ist eingebaut und braucht keine Einrichtung. Ausschalten kannst du sie unter Einstellungen → Geräte & Dienste → Mira → **Konfigurieren** → **Allgemein** → **Internetsuche**, etwa wenn du eine andere Suche verwendest (siehe „Weitere Werkzeuge über MCP“).

Ist die Einrichtung mit einem stärkeren Sprachmodell verbunden, gibt Mira aufwendige Fragen dorthin ab – Erklärungen, Planungen, Rezepte, Vergleiche – und fasst das Ergebnis für dich zusammen. Das stärkere Modell kann nachschlagen, aber nichts im Haus verändern.

## Erinnerungen, Timer und Wecker

- „Erinnere mich morgen um 19 Uhr daran, den Müll rauszubringen.“
- „Stell einen Timer auf 10 Minuten.“ – „In einer halben Stunde erinnerst du mich an die Wäsche.“
- „Welche Erinnerungen habe ich?“ – „Lösch die Erinnerung an die Wäsche.“

▶ [Eine Erinnerung](https://mira-assist.com/videos/mira-chat-de.mp4#t=64,98)

▶ [Einen Wecker stellen, ändern und wieder löschen](https://mira-assist.com/videos/mira-chat-de.mp4#t=139,194)

Zur gewählten Zeit sagt Mira die Erinnerung in dem Raum an, in dem du sie gestellt hast – auch in der Ruhezeit, denn die Zeit hast du gewählt. Per Telegram oder in der HA-App gestellte Erinnerungen kommen privat über deine Kanäle.

**Wecker**: „Weck mich morgen um 6“ – „Weck mich werktags um halb sieben“ – „Weck mich zur üblichen Zeit“, wenn Mira die kennt. Der Wecker klingelt an dem Satelliten, an dem du gefragt hast, und zwar auf dem Gerät selbst: Schlummern und Ausschalten gehen dort wie gewohnt, und er klingelt auch, wenn Home Assistant gerade nicht läuft. Mira fragt nicht nach, sondern sagt, was sie gestellt hat; hat sie sich verhört, sag gleich „Nein, halb sieben“. Das geht an Satelliten, die Wecker können, etwa einem Echo Show mit TECHO5; an anderen sagt Mira, dass es dort nicht geht.

## Nachrichten an andere

- „Sag Anna, dass es später wird.“ – Anna hört: „Ben lässt dir ausrichten: Es wird später.“
- „Hinterlass fürs Haus: Der Schornsteinfeger kommt um 10.“ – wer als Nächstes in einen Raum kommt, hört es (oder die Anzeige zeigt es).
- „Erinnere Ben morgen um 7 an den Müll.“ – eine Nachricht, die erst dann zugestellt wird.
- „Schick mir das aufs Handy.“ – kommt über deine Kanäle.

▶ [Eine zweite Nachricht fürs Haus: Mira hängt sie an die nächste Antwort an](https://mira-assist.com/videos/mira-chat-de.mp4#t=228,265)

An eine Person geht eine Nachricht über ihre Kanäle (Telegram, Companion-App), am Satelliten nur, wenn sie dort als sie erkannt ist. Jede Person stellt ein, von wem sie Nachrichten annimmt: von allen, nur von Personen, die Mira erkennt, oder von niemandem; Miras eigene Nachrichten (Aufgaben, Erinnerungen, Warnungen) kommen immer an. Wer etwas von jemandem im Wohnzimmer hinterlässt, ohne erkannt zu sein, erscheint als „jemand im Bereich Wohnzimmer“. Ob eine Nachricht gehört wurde, erfährt der Absender nicht.

Eine Nachricht fürs Haus, die du an einem Satelliten hinterlässt, wird in genau diesem Raum zehn Minuten lang nicht angesagt oder angezeigt: Du stehst ja dort. Alle anderen Räume bekommen sie sofort. Die Zeit stellst du in Home Assistant unter „Konfigurieren → Allgemein“ ein; 0 heißt: nicht warten. Dringendes und Erinnerungen sind davon nicht betroffen.

Eine **Durchsage** („Sag allen, das Essen ist fertig“) ist etwas anderes: Sie geht sofort über alle Satelliten und wird nicht gespeichert.

## Und natürlich: das Haus steuern

- „Mach im Wohnzimmer das Licht an.“ – „Dimm die Stehlampe auf 30 Prozent.“
- „Wie warm ist es im Bad?“ – „Welche Fenster sind offen?“ – „Wie wird das Wetter morgen?“

▶ [Eine Lampe in Farbe und Helligkeit steuern, auch mit „meine Lieblingsfarbe“](https://mira-assist.com/videos/mira-chat-de.mp4#t=471,536)

Mira fragt Werte immer frisch ab, statt sie zu schätzen. Kann sie etwas nicht nachsehen, sagt sie das ehrlich.

**Wichtige Aktionen bestätigen**: Schlösser, Alarmanlage, Garagentor und Ähnliches führt Mira erst aus, wenn du auf ihre Rückfrage ausdrücklich „Ja“ sagst.

## Timer

„Stell einen Timer auf 10 Minuten für die Nudeln.“ – „Wie lange noch?“ – „Noch fünf Minuten dazu.“ – „Timer anhalten.“ – „Timer abbrechen.“

Das sind die Timer von Home Assistant, und sie laufen auf dem Satelliten, an dem du sprichst: Er zählt herunter, zeigt den Rest (der Show auf dem Bildschirm), klingelt, und du stoppst ihn mit „Stopp“. Mira baut keinen eigenen Timer, sie nutzt die Werkzeuge, die Home Assistant seinen Sprachassistenten dafür gibt. Mehrere Timer unterscheidet der Name („den Nudel-Timer“). Auf einem Gerät ohne Timer, etwa in Telegram, sagt Mira das ehrlich.

Eine **Erinnerung** („Erinnere mich in einer Stunde an die Wäsche“) ist etwas anderes: Mira sagt dir dann den Text.

## Einschlaf-Timer

„Schlaf-Timer 30 Minuten.“ – „Schalte in einer halben Stunde die Musik aus, und das Licht auch.“ – „Noch zehn Minuten.“ – „Doch abbrechen.“ – „Welche Timer habe ich?“

Nach der Zeit hört im Raum die Wiedergabe auf (Hörbuch, Radio, Musik), auf Wunsch geht auch das Licht aus. Ein zweiter Schlaf-Timer im selben Raum ersetzt den ersten. Du kannst höchstens zehn offene Timer je Person und Raum haben.

Der Timer läuft in **Home Assistant**, nicht in Mira: Er wird dort als einmalige Automation mit dem Label „Mira: einmalig“ angelegt und räumt sich nach dem Ablauf selbst auf. Ist Home Assistant zur Zeit aus, holt er es beim Start nach, damit das Licht nicht die ganze Nacht brennt. Der Lautsprecher und das Licht müssen für Assist freigegeben sein. Schlösser, Alarmanlage und Garagentor schaltet ein Timer nie.

## Szenen und Skripte aus Home Assistant

Szenen („Gute Nacht“, „Heimkino“) und Skripte, die du in Home Assistant angelegt und für Assist freigegeben hast, kann Mira starten:

- „Gute Nacht.“ – „Schalte die Szene Heimkino ein.“ – „Ich will mit Regen einschlafen.“

Wie Mira den richtigen Ablauf erkennt, hängt an zwei Angaben in Home Assistant:

- **Der Name** sollte sagen, was passiert: „Gute Nacht“, „Heimkino“, „Einschlafen mit Regen“.
- **Bei Skripten die Beschreibung** (`description` im Skript): ein Satz, was es tut, zum Beispiel „Spielt leise Regengeräusche zum Einschlafen auf dem Show im Wohnzimmer.“ Mira liest diesen Satz. Ohne ihn hat sie nur den Namen, und bei einem Wunsch wie „Mach Regengeräusche an“ kann sie sich verschätzen und stattdessen einen Radiosender suchen.

Bei Szenen reicht der Name. Wichtige Aktionen (Schlösser, Alarmanlage, Garagentor) in einem Ablauf fragen weiter nach, wie überall.

## Mira in Home Assistant

Mira steht in Home Assistant im Seitenmenü. Du bist dort schon angemeldet, sie sieht aus wie dein HA-Theme (hell oder dunkel) und spricht deine HA-Sprache; das klappt auch unterwegs in der App.

- **Nachrichten**: offene und zugestellte Nachrichten ansehen und quittieren.
- **Aufgaben**: was Mira für dich im Blick behält; an/aus, Probelauf, Protokoll, löschen, „In Home Assistant bearbeiten“.
- **Benachrichtigung**: Ruhezeit, Pausen, Eskalation, Vertrauliches, dringende Nachrichten.
- **Satelliten** und **Personen**: alle aus Home Assistant, mit dem, was Mira zusätzlich braucht; als Admin stellst du es hier direkt ein (siehe unten).
- **Gedächtnis**: das des Hauses und dein eigenes; bearbeiten und vergessen.
- **Anleitung**: diese Seite.

Wer kein Admin ist, sieht nur **Gedächtnis** und **Anleitung**. Damit Mira weiß, wessen Gedächtnis du bist, muss deine Person mit deinem HA-Benutzer verknüpft sein (Einstellungen → Personen).

![Benachrichtigung: Ruhezeit, Pausen, Eskalation](img/benachrichtigung-de.png)

Bevor Mira etwas löscht, fragt sie nach:

![Rückfrage vor dem Vergessen eines Fakts](img/dialog-vergessen-de.png)

**Mira für sich allein**: Dieselbe Oberfläche gibt es auch direkt bei Mira unter `http://<mira>:8787/ui`, gedacht für den, der Mira betreibt. Sie macht ein paar Dinge anders: Sie kommt in Miras eigenen Farben, fragt beim ersten Aufruf nach dem Zugangs-Token, zeigt die Gedächtnisse aller Personen und lässt dort Satelliten und Personen bearbeiten, solange die Integration sie nicht verwaltet.

## In Home Assistant

- **Gerät „Mira“** mit dem Schalter **Ruhemodus**, dem Sensor **Wartende Nachrichten** und der Taste **Profil verdichten**; die Versionen von Integration und Agent stehen in den Geräte-Informationen.
- Mira ist ein Gesprächsagent: Unter Einstellungen → Sprachassistenten wählst du sie für deine Satelliten aus.
- Automationen können Mira Nachrichten geben: mit HAs eigener Aktion `notify.send_message` an **Mira Haus** oder **Mira ‹Person›** (je eine notify-Entität fürs Haus und pro Person), oder mit `mira.send_message`, wenn es mehr braucht (Wichtigkeit, vertraulich, Ablauf).
- **Mira im Seitenmenü** (siehe oben). Ausschalten in den Optionen der Integration; jeder kann sie auch nur in seinem eigenen Seitenmenü ausblenden.

![Das Gerät „Mira“ in Home Assistant](img/ha-geraet-de.png)

Mira kennt deine Personen und Satelliten von selbst, anlegen musst du nichts: Jede Person aus Home Assistant ist auch für Mira eine Person, und jeder Assist-Satellit spricht in seinem Bereich.

![Die Mira-Integration in Home Assistant](img/ha-integration-de.png)

Was Mira darüber hinaus braucht, stellst du am einfachsten im Mira-Panel unter **Satelliten** und **Personen** ein; dasselbe geht in Home Assistant unter Einstellungen → Geräte & Dienste → Mira → **Konfigurieren**, mit einem aufklappbaren Abschnitt pro Person oder Satellit. Gespeichert wird es direkt an der Person oder am Satelliten, und es verschwindet mit ihnen.

- **Einstellungen für einen Satelliten**: Bewegungsmelder (auch mehrere, etwa Flur und Wohnzimmer), beim Hereinkommen ansagen, Anzeige (LED-Ring). Der Raum ist der Bereich des Satelliten.
- **Einstellungen für eine Person**: **Kanäle** (notify-Ziele, z. B. der Telegram-Chat aus HAs Telegram-Integration und die Companion-App; persönliche Nachrichten gehen an den ersten, der klappt), **Nachrichten annehmen von** (allen, nur Personen, niemandem) und „Allein zu Hause sind die Satelliten ich“.

**Zwei Arten von notify-Entitäten:** **Mira Haus** und **Mira ‹Person›** (`notify.mira_…`) sind Miras Briefkästen. Was dort ankommt, wird eine Nachricht, und Mira entscheidet, wie sie ankommt: angesagt, angezeigt oder privat, mit Ruhezeit und Wichtigkeit. Die **Kanäle** einer Person sind dagegen Transporte, die direkt zustellen, etwa der Telegram-Chat oder die Companion-App. Automationen schicken an die Briefkästen, als Kanal trägst du Transporte ein; Miras eigene Briefkästen bietet die Auswahl dort gar nicht erst an.

![Einstellungen für Personen unter „Konfigurieren“: ein Abschnitt pro Person](img/ha-person-de.png)

- **Persönlichkeit** (unter **Allgemein**): wie Mira klingt, als freier Text: Ton, Humor, wie knapp sie bei Gerätebefehlen antwortet, ein paar Beispiele für ihren Ton. Sie hängt ihn an ihre festen Regeln an, er gilt für Sprache, Telegram und ihre Ansagen. Was je Kanal gilt (kurze Sätze am Satelliten, Listen bei Telegram) und welche Sprache, regelt sie selbst; das gehört nicht hinein.
- **Nachfragen**: Hängst du in Home Assistant das Label **„Mira: nachfragen“** an eine Entität, etwa das Garagentor, schaltet Mira sie nur nach deinem „Ja“, und Aufgaben schalten sie nie. Schlösser und Alarmanlagen sind das ohnehin.

Im Assist-Chat der HA-App und im Seitenmenü erkennt Mira dich an deinem HA-Benutzer, über die Person, mit der Home Assistant ihn verknüpft.

## Weitere Werkzeuge über MCP

Mira kann zusätzliche Werkzeuge nutzen, die ein MCP-Server anbietet, etwa eine andere Websuche als die eingebaute. Du trägst sie in `mira.yaml` unter `mcp_servers` ein (Home Assistants eigener MCP-Server ist immer dabei); Mira verbindet sich beim Start und gibt dem Modell die Werkzeuge mit, mit dem Servernamen davor (`search__…`). Das funktioniert mit jedem Sprachmodell.

Ein Server läuft entweder für sich und wird über HTTP erreicht, oder Mira startet ihn selbst als Programm (stdio):

```yaml
mcp_servers:
  - name: search                   # Mira startet den Server selbst
    transport: stdio
    command: /usr/bin/node
    args: ["/opt/web-search-mcp/dist/index.js"]
    env: ["MAX_RESULTS=5"]         # optional
    read_only: true
    web_content: true
  - name: notizen                  # läuft für sich, über HTTP
    transport: http
    url: "http://localhost:9100/mcp"
    token: ${NOTIZEN_TOKEN}        # optional
    disabled: true                 # vorübergehend aus
```

Zwei Schalter entscheiden, wie vorsichtig Mira mit einem Server umgeht:

- **`read_only: true`**: Der Server liest nur und ändert nichts. Nur solche Werkzeuge stehen auch Hintergrundarbeiten (Beurteilen von Aufgaben, abgegebene schwierige Fragen) zur Verfügung. Ohne den Schalter gibt es die Werkzeuge nur im Gespräch.
- **`web_content: true`**: Was der Server liefert, ist fremder Text, zum Beispiel aus Webseiten, und könnte versteckte Anweisungen enthalten. Sobald Mira ein solches Werkzeug benutzt hat, sind für den Rest der Anfrage kritische Aktionen gesperrt: alles, was unter `security` als bestätigungspflichtig eingetragen ist, etwa Schlösser, Alarmanlage, Garage.

Ein Programm, das Mira selbst startet (stdio), bekommt nur das Nötigste aus ihrer Umgebung: Suchpfad, Heimatordner, Sprache und Proxy-Einstellungen. Ihre Zugangsdaten (Home-Assistant-Token, Modell-Schlüssel) bekommt es nicht. Was der Server braucht, trägst du unter `env` ein, zum Beispiel `env: ["API_KEY=${BRAVE_KEY}"]`.

Neue oder geänderte Server gelten nach einem Neustart von Mira. `mira check` prüft, ob alle erreichbar sind.

## Einrichtung

Du brauchst zwei Dinge:
- **Einen Rechner mit Docker** in deinem Heimnetz. Das kann derselbe sein, auf dem Home Assistant läuft. Mit Home Assistant OS nimmst du vorerst einen anderen Rechner; ein Add-on kommt später.
- **Ein KI-Modell (LLM)**, mit dem Mira denkt. Am einfachsten ist ein Cloud-Anbieter wie Google Gemini oder OpenAI. Hast du in Home Assistant schon eine KI-Integration eingerichtet, übernimmt Mira sie samt Schlüssel. Ein eigener Server (LM Studio, Ollama, llama.cpp) geht auch, braucht aber eine starke Grafikkarte.

Eine Config-Datei brauchst du nicht. Drei Schritte:

**1. Die Integration installieren.** In Home Assistant über HACS: Benutzerdefinierte Repositories → `https://github.com/miraassist/mira`, Typ Integration. **Mira** installieren, Home Assistant neu starten.

**2. Mira starten.** Auf dem Docker-Rechner, eine Zeile:

```sh
docker run -d --name mira --restart unless-stopped --network host \
  -e TZ=Europe/Berlin -v mira-data:/var/lib/mira ghcr.io/miraassist/mira
```

**3. Mira hinzufügen.** Einstellungen → Geräte & Dienste. Mira steht unter **Entdeckt** → **Hinzufügen**. Der Dialog zeigt eine Übersicht: was schon gewählt ist und was noch fehlt. Von dort aus wählst du, und jeder Schritt führt zurück zur Übersicht:
- **KI-Modell (LLM)**, mit dem Mira denkt:
  - **Aus Home Assistant**: eine KI-Integration, die du schon hast; nichts weiter einzugeben.
  - **OpenAI, Google Gemini, OpenRouter, Mistral**: dein API-Schlüssel vom Anbieter.
  - **Eigener Server**: seine Adresse aus Sicht von Mira (`localhost` ist der Rechner, auf dem Mira läuft).

  Dann das Modell aus der Liste. Mira probiert es gleich aus.
- **Mira verwenden in**: die Assistenten, in denen Mira antwortet; der bevorzugte ist schon angehakt.
- **Mira jetzt nutzen**: erscheint, sobald nichts mehr fehlt. Fertig.

Schließt du den Dialog zwischendurch, geht es beim nächsten Öffnen in der Übersicht weiter.

Steht Mira nicht unter „Entdeckt“: **Integration hinzufügen** → Mira, und die Adresse eingeben, etwa `http://192.168.1.20:8787`.

Dabei richtet die Integration alles Übrige in Home Assistant selbst ein:
- einen Benutzer **„Mira“** ohne Admin-Rechte, mit dem Mira Home Assistant anspricht;
- den **MCP-Server** von Home Assistant, falls er noch fehlt;
- **Mira als Gesprächsagent** in den Assistenten, die du angehakt hast.

Vorher passiert nichts. Brichst du den Dialog ab, bleibt nichts halb eingerichtet zurück.

Was Mira sehen und schalten darf, bestimmst du unter Einstellungen → Sprachassistenten → **Entitäten freigeben**.

### Wenn etwas hakt

Der Dialog sagt, was nicht stimmt, und du kannst es gleich dort korrigieren:
- **„Der Anbieter hat den Schlüssel abgelehnt“**: Schlüssel prüfen und neu eingeben.
- **„Mira erreicht diese Adresse nicht“** (eigener Server):
  - Prüfe die Adresse aus Sicht von Mira.
  - Läuft LM Studio auf einem anderen Rechner, schalte dort „Serve on Local Network“ ein.
  - Ollama hört von Haus aus nur auf seinen eigenen Rechner (`OLLAMA_HOST=0.0.0.0` ändert das).
- **„Das Modell benutzt keine Werkzeuge“**: Wähle ein anderes Modell. Mira braucht Werkzeuge zum Schalten und Nachschauen.

Und sonst:
- **Mira steht nicht unter „Entdeckt“?** Meist fehlt `--network host`, oder Mira steht in einem anderen Netz. Dann gibst du die Adresse von Hand ein (siehe Schritt 3).
- **Der Dialog fragt nach einem Kopplungscode?** Diese Mira ist schon mit einem Home Assistant gekoppelt. Entfernst du dort die Integration, gibt sie Mira frei, und es geht wieder ohne Code. Sonst bekommst du einen Code mit `docker exec mira /app/mira pair`.
- **Anderes Modell?** Einstellungen → Geräte & Dienste → Mira → **Konfigurieren** → **KI-Modell (LLM)**.

**Sicherheit beim Einrichten:** Eine neue Mira koppelt sich mit dem ersten Home Assistant, der das Koppeln abschließt, ohne Code. Richte sie deshalb im eigenen Heimnetz ein, nicht in einem Gästenetz oder geteilten Netz. Hätte dir jemand zuvorgekommen, merkst du es sofort: Dein Dialog fragt dann nach einem Code.

Für Fortgeschrittenes (mehrere Modelle, weitere MCP-Server, Skills mit Einstellungen) gibt es die Datei `mira.yaml`, siehe [mira.example.yaml](https://github.com/miraassist/mira/blob/main/mira.example.yaml). Was du in der Integration einstellst, gilt vor der Datei.

## Sicherheit: wie Mira und Home Assistant miteinander sprechen

- **Zwei Schlüssel:**
  - Home Assistant spricht Mira mit einem **Zugangs-Token** an. Den holt sich die Integration beim Hinzufügen, du siehst ihn nie.
  - Mira spricht Home Assistant als **eigener Benutzer ohne Admin-Rechte** an.
  - Wer den Zugangs-Token kennt, kann alles, was Mira kann. Behandle ihn wie ein Passwort.
- **Was Mira in HA darf:** nichts verwalten (keine Benutzer, keine Integrationen), aber Geräte schalten. Was sie sieht, bestimmst du mit „Für Assist freigeben“. Schlösser, Alarmanlage und alles mit dem Label „Mira: nachfragen“ schaltet sie nur nach deinem Ja.
- **Läuft Mira auf demselben Rechner wie Home Assistant** (der Normalfall), bleibt alles auf diesem Rechner.
- **Läuft Mira auf einem anderen Rechner** in deinem Netz, geht die Verbindung unverschlüsselt durchs Heimnetz: der Zugangs-Token, was du sagst und was Mira antwortet. Im eigenen Heimnetz ist das üblich, ein Gästenetz oder ein geteiltes Netz ist dafür nicht geeignet.

## Gut zu wissen

- Mira erfindet keine Werte: Fehlt ihr etwas, sagt sie es oder fragt nach.
- Nach einer Websuche führt Mira keine kritischen Aktionen aus, bis du den Befehl noch einmal direkt gibst. Etwas zu merken, eine Nachricht zu schicken oder eine Aufgabe anzulegen macht sie dann erst nach deinem Ja. So kann eine Webseite ihr nichts unterschieben, was sie dauerhaft behält oder weitergibt.
- Bei Spracherkennungsfehlern hilft es, deutlich und in einem Satz zu sprechen.
- Ein Gespräch per Telegram läuft weiter, solange du schreibst. Mira sieht davon die letzten Wechsel (etwa 30 bis 50) und alles zum Anliegen, an dem ihr gerade arbeitet; Älteres fällt heraus, was sie sich gemerkt hat, bleibt. `/neu` beginnt von vorn.
