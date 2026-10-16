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

> 🎬 **Aufnahme folgt:** ein Sprachdialog am Satelliten – eine Frage, für die Mira im Internet nachschaut, eine Nachfrage dazu, und am Ende ein Auftrag fürs Haus.

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
- **Per Telegram**: schreib dem Mira-Bot. Antworten dürfen hier länger sein, mit Listen und Links. `/neu` beginnt ein neues Gespräch.
- **In der Home-Assistant-App**: Der Assist-Chat der Companion App spricht ebenfalls mit Mira, getippt oder gesprochen.

Mira antwortet in der Sprache, in der du sie ansprichst.

## Mira passt aufs Haus auf: Aufgaben

Gib Mira eine dauerhafte Aufgabe, und sie behält etwas für dich im Blick:

- „Achte auf den Gasverbrauch und sag Bescheid, wenn in einer Stunde ungewöhnlich viel verbraucht wird.“
- „Sag Bescheid, wenn nachts ungewöhnlich viel Strom verbraucht wird.“
- „Melde dich sofort, wenn einer der Lecksensoren anschlägt.“

So läuft das Einrichten ab:

1. Fehlt Mira etwas, fragt sie zurück, statt zu raten – etwa „Welche Uhrzeit ist für dich ungewöhnlich?“. Antworte einfach; das Gespräch bleibt beim Thema, auch über mehrere Sätze.
2. Dann plant sie. Geht das schnell, nennt sie dir den Vorschlag gleich; sonst sagt sie „Ich plane das“ und meldet sich, sobald sie fertig gesprochen hat – im selben Raum oder auf Telegram.
3. Der Vorschlag sagt, was sie beobachtet, und nennt die Bedingung: „Wenn die Nachttischlampe angeht, sage ich dir Bescheid. Bedingung: nur zwischen 1 und 4 Uhr. Soll ich so vorgehen?“
4. Sag **„Ja“**, dann richtet sie die Aufgabe ein. Mit „Nein, lass es“ bricht sie ab. Antwortet niemand, verfällt der Vorschlag nach einer Viertelstunde.

Beim Beurteilen vergleicht Mira zum Beispiel den Verbrauch der letzten Stunde mit derselben Stunde der vergangenen zwei Wochen, weiß, wer zu Hause ist, kennt, was du ihr gemerkt hast, und erinnert sich, was sie zu dieser Aufgabe schon gesagt hat. Dieselbe Sache meldet sie nicht ständig wieder, nur wenn es schlimmer wird.

- „Welche Aufgaben hast du?“ – „Pausier die Gas-Aufgabe.“ – „Lösch die Aufgabe mit dem Strom.“ – „Mach einen Probelauf der Gas-Aufgabe.“
- In der Web-Oberfläche unter **Aufgaben** siehst du alle Aufgaben mit an/aus, Probelauf und dem Protokoll ihrer Urteile, dazu Vorschläge, die noch auf dein Ja warten.

![Aufgaben in der Web-Oberfläche: ein wartender Vorschlag, eine pausierte und zwei laufende Aufgaben](img/aufgaben-liste-de.png)

Unter der Haube ist jede Aufgabe eine ganz normale **Automation in Home Assistant** mit dem Label „Mira“: Home Assistant entscheidet, *wann* etwas passiert (stündlich, wenn ein Sensor auslöst, nur nachts …), Mira entscheidet, *was* davon dich interessiert. „In Home Assistant bearbeiten“ öffnet sie im Editor; dort kannst du sie auch selbst ändern.

![Die Gas-Aufgabe im Automations-Editor von Home Assistant: stündlicher Auslöser, Aktion „Mira: Bewerten“](img/ha-automation-de.png)

Eigene Automationen können Mira genauso nutzen: die Aktionen **Mira: Bewerten** (`mira.assess`), **Mira: Entscheiden** (`mira.decide`, eine Ja/Nein-Antwort für Bedingungen) und **Mira: Hinweis hinzufügen** (`mira.add_notice`). Aufgaben gehören vorerst dem ganzen Haus und nutzen beim Beurteilen das Haus-Gedächtnis.

## Mira meldet sich von selbst

Mira meldet sich, wenn etwas passiert: ein Lecksensor schlägt an, die Waschmaschine ist fertig, morgen wird der Müll abgeholt, eine Aufgabe findet etwas Ungewöhnliches. Wie sie dir das sagt, hängt vom Raum und von der Wichtigkeit ab.

> 🎬 **Aufnahme folgt:** Die Anzeige-Lampe pulsiert, du fragst „Hast du eine Benachrichtigung?“, Mira liest vor.

**Ansagen oder anzeigen** – pro Raum einstellbar:

- **Ansagen**: Sobald jemand den Raum betritt (Bewegungsmelder), sagt Mira an, was ansteht. Die erste Bewegung nach der Ruhezeit bringt die Morgenansage.
- **Nur anzeigen**: Mira spricht dich nicht von sich aus an. Die Anzeige-Lampe des Raums (z. B. der LED-Ring des Echo) pulsiert in Türkis, solange etwas wartet. Frag dann: **„Hast du eine Benachrichtigung?“** oder „Gibt es was Neues?“.

**Nebenbei**: Wenn du ohnehin mit dem Satelliten sprichst, hängt Mira Wartendes an ihre Antwort an – „Das Licht ist an. Übrigens: Morgen wird der Wertstoff abgeholt.“ Danach gilt die Meldung als gehört und die Lampe geht aus. Fragst du nach („Was genau?“), bekommst du die Details.

**Wichtigkeit**:

| Stufe | Bedeutung | Was passiert |
|---|---|---|
| 1 | nebenbei | wartet auf die nächste Gelegenheit |
| 2 | heute wichtig | geht morgens nach 9 Uhr privat raus, wenn bis dahin niemand sie gehört hat |
| 3 | dringend | sofort, auch nachts: angesagt auf allen Satelliten und privat an alle |

Wenn Mira von sich aus spricht, leuchtet die Anzeige-Lampe des Raums ruhig, solange sie spricht.

**Ruhezeit**: In der Ruhezeit (z. B. 23:30–6:30) sagt Mira nichts an – außer Dringendem und deinen Erinnerungen. Anzeige-Lampen leuchten trotzdem; was nachts kommt, siehst du morgens.

**Ruhemodus**: Der Schalter „Ruhemodus“ in Home Assistant stellt alle Ansagen ab, bis du ihn wieder ausschaltest.

**Vertrauliches und Persönliches**: Meldungen können für eine Person bestimmt oder vertraulich sein. Vertrauliches liest Mira nur vor, wenn die Einstellung es erlaubt (z. B. nur wenn die Person allein zu Hause ist); sonst kommt es privat per Telegram oder als Push-Nachricht aufs Handy.

![Meldungen in der Web-Oberfläche: wartende und zugestellte Meldungen](img/meldungen-de.png)

## Mira merkt sich Dinge

- „Merk dir, dass der Schornsteinfeger im März kommt.“
- „Merk dir mal was.“ – Mira fragt „Was denn?“, und du sagst es im nächsten Satz.
- „Was weißt du über den Schornsteinfeger?“ – „Vergiss das mit dem Schornsteinfeger.“

Was Mira weiß, nutzt sie auch beim Aufpassen: Sag ihr „Merk dir, die Nachttischlampe nach Mitternacht ist ungewöhnlich“, und ihre Aufgaben beurteilen danach.

Es gibt ein Gedächtnis fürs **Haus** und eins für **jede Person**:

- Weiß Mira, wer spricht, merkt sie sich es für diese Person. Ins Haus-Gedächtnis kommt es nur, wenn du es ausdrücklich sagst („merk dir fürs Haus …“).
- Mira weiß, wer spricht: über Telegram, im Chat der HA-App (über deinen HA-Benutzer) und am Satelliten nur, wenn du allein zu Hause bist und das bei deiner Person eingeschaltet hast („Allein zu Hause sind die Satelliten ich“). Sonst ist es am Satelliten das Haus.
- Dein persönliches Gedächtnis sieht nur Mira im Gespräch mit dir. Das Haus-Gedächtnis gilt für alle.
- In fremde Gedächtnisse schreibt niemand.

Nachts fasst Mira jedes Gedächtnis zu einem kurzen Profil zusammen. In der Web-Oberfläche unter **Gedächtnis** siehst du Haus und Personen getrennt, mit wer es wann gesagt hat, und kannst Fakten bearbeiten oder löschen.

![Das Gedächtnis: Haus, Anna und Ben, jeweils mit Urheber und Zeitpunkt](img/gedaechtnis-de.png)

## Skills: zum Beispiel die Müllabfuhr

Skills sind Zusatzfähigkeiten, die die Einrichtung freischaltet. Mira öffnet einen Skill nur, wenn eine Frage ihn braucht.

**Abfall**: Mira kennt den Abfuhrkalender deines Entsorgers.

- „Wann wird der Restmüll abgeholt?“ – „Was wird nächste Woche abgeholt?“
- Am Vorabend erinnert Mira von selbst: „Morgen wird der Wertstoff abgeholt.“
- Ist die Abfuhr heute, sagt sie es, falls die Tonne noch raus muss.

> 🎬 **Aufnahme folgt:** „Was wird nächste Woche abgeholt?“ am Satelliten.

## Im Internet nachschauen und schwierige Fragen

Ist eine Websuche eingerichtet, schaut Mira nach, was sie nicht wissen kann: Öffnungszeiten, Ergebnisse, Neuigkeiten. Sie fasst es kurz zusammen und sagt, wenn sie nichts Verlässliches gefunden hat. Was im Internet steht, behandelt sie als Information, nie als Anweisung.

Ist die Einrichtung mit einem stärkeren Sprachmodell verbunden, gibt Mira aufwendige Fragen dorthin ab – Erklärungen, Planungen, Rezepte, Vergleiche – und fasst das Ergebnis für dich zusammen. Das stärkere Modell kann nachschlagen, aber nichts im Haus verändern.

## Erinnerungen und Timer

- „Erinnere mich morgen um 19 Uhr daran, den Müll rauszubringen.“
- „Stell einen Timer auf 10 Minuten.“ – „In einer halben Stunde erinnerst du mich an die Wäsche.“
- „Welche Erinnerungen habe ich?“ – „Lösch die Erinnerung an die Wäsche.“

Zur gewählten Zeit sagt Mira die Erinnerung in dem Raum an, in dem du sie gestellt hast – auch in der Ruhezeit, denn die Zeit hast du gewählt. Per Telegram oder in der HA-App gestellte Erinnerungen kommen privat zu dir, per Telegram oder als Push-Nachricht.

## Und natürlich: das Haus steuern

- „Mach im Wohnzimmer das Licht an.“ – „Dimm die Stehlampe auf 30 Prozent.“
- „Wie warm ist es im Bad?“ – „Welche Fenster sind offen?“ – „Wie wird das Wetter morgen?“

Mira fragt Werte immer frisch ab, statt sie zu schätzen. Kann sie etwas nicht nachsehen, sagt sie das ehrlich.

**Wichtige Aktionen bestätigen**: Schlösser, Alarmanlage, Garagentor und Ähnliches führt Mira erst aus, wenn du auf ihre Rückfrage ausdrücklich „Ja“ sagst.

## Web-Oberfläche

In Home Assistant steht Mira für Admins im Seitenmenü, mit deiner HA-Anmeldung und im Aussehen deines HA-Themes, auch von unterwegs. Für sich allein läuft sie unter `http://<mira>:8787/ui` (Zugangs-Token beim ersten Aufruf). Beide zeigen dasselbe:

- **Meldungen**: offene und zugestellte Meldungen ansehen und quittieren.
- **Aufgaben**: was Mira für dich im Blick behält; an/aus, Probelauf, Protokoll, löschen, Link in den Editor von Home Assistant.
- **Benachrichtigung**: Ruhezeit, Pausen, Eskalation, Vertrauliches, dringende Meldungen.
- **Räume**: welcher Satellit wo spricht, Bewegungsmelder, ansagen oder anzeigen, Anzeige-Lampe.
- **Personen**: wer persönliche Meldungen bekommt, per Telegram oder Push, und ob die Satelliten als diese Person gelten, wenn sie allein zu Hause ist („Allein = ich“).
- **Gedächtnis**: Haus und jede Person getrennt, mit Profil, Fakten, wer sie wann gesagt hat; bearbeiten und vergessen.
- **Anleitung**: diese Seite.

![Einstellungen für Benachrichtigungen: Ruhezeit, Pausen, Eskalation](img/benachrichtigung-de.png)

Bevor Mira etwas löscht, fragt sie nach:

![Rückfrage vor dem Vergessen eines Fakts](img/dialog-vergessen-de.png)

Ist die Mira-Integration in Home Assistant eingerichtet, bearbeitest du Räume und Personen dort (Einstellungen → Geräte & Dienste → Mira) mit der Auswahl von Entitäten und Bereichen; die Web-Oberfläche zeigt sie dann nur an.

## In Home Assistant

- **Gerät „Mira“** mit dem Schalter **Ruhemodus**, dem Sensor **Offene Hinweise** und der Taste **Profil verdichten**; die Versionen von Integration und Agent stehen in den Geräte-Informationen.
- Mira ist ein Gesprächsagent: Unter Einstellungen → Sprachassistenten wählst du sie für deine Satelliten aus.
- Automationen können Mira Meldungen geben, mit der Aktion `mira.add_notice`.
- **Mira im Seitenmenü**: die Web-Oberfläche als Seite in Home Assistant, für Admins. Ausschalten in den Optionen der Integration; jeder kann sie auch nur in seinem eigenen Seitenmenü ausblenden.

![Das Gerät „Mira“ in Home Assistant](img/ha-geraet-de.png)

Personen und Räume richtest du unter Einstellungen → Geräte & Dienste → Mira ein:

![Die Mira-Integration mit ihren Personen und Räumen](img/ha-integration-de.png)

Pro Person: Personen-Entität, Telegram-ID, Push-Dienst und „Allein zu Hause sind die Satelliten ich“. Im Assist-Chat der HA-App erkennt Mira dich an deinem HA-Benutzer, wenn deine Personen-Entität mit ihm verknüpft ist.

![Eine Person einrichten](img/ha-person-de.png)

## Weitere Werkzeuge über MCP

Mira kann zusätzliche Werkzeuge nutzen, die ein MCP-Server anbietet, etwa eine Websuche. Die Einrichtung trägt sie in `mira.yaml` unter `mcp_servers` ein; Mira verbindet sich beim Start und gibt dem Modell die Werkzeuge mit, mit dem Servernamen davor (`search__…`). Das funktioniert mit jedem Sprachmodell.

Ein Server läuft entweder für sich und wird über HTTP erreicht, oder Mira startet ihn selbst als Programm (stdio):

```yaml
mcp_servers:
  - name: ha                       # Home Assistant, ist immer dabei
    transport: http
    url: "http://homeassistant.local:8123/api/mcp/assist"
    token: ${HA_TOKEN}
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

Neue oder geänderte Server gelten nach einem Neustart von Mira. `mira check` prüft, ob alle erreichbar sind.

## Gut zu wissen

- Mira erfindet keine Werte: Fehlt ihr etwas, sagt sie es oder fragt nach.
- Nach einer Websuche führt Mira keine kritischen Aktionen aus, bis du den Befehl noch einmal direkt gibst.
- Bei Spracherkennungsfehlern hilft es, deutlich und in einem Satz zu sprechen.
