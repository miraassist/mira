# Plan: weitere Werkzeuge und Skills

Stand 2026-10-17, beschlossen, nicht gebaut. Gleiches Muster wie [Kalender](kalender.md): Was HA über Assist schon kann, baut Mira nicht nach. Wie Daten dargestellt werden (Stufen, Zeitachse): [Kontext](kontext.md), Abschnitt 2. Für den Rest gilt:
- **Lesen** bekommt ein eigenes Werkzeug mit knapper Darstellung: Mira bestimmt, wie das Modell die Daten sieht, statt rohe Mengen weiterzureichen.
- **Handeln** wird eine [Skill-Aktion](skill-aktionen.md), möglichst ohne Code.

Assist bietet heute schon:
- Licht, Schalten, Rollos, Klima, Lüfter, Staubsauger;
- Medien (Pause, Weiter, Lautstärke, `search_media`, `play_media`);
- To-do (lesen, hinzufügen, abhaken), Durchsage, Zustände (`GetLiveContext`), Datum und Uhrzeit;
- freigegebene Skripte.

## Reihenfolge

0. **Kalender und To-do**: siehe [kalender.md](kalender.md).
1. **Verlauf und Logbuch**: Werkzeug mit Ansicht.
2. **Wetter-Vorhersage**: Werkzeug mit Ansicht.
3. **Musik über Music Assistant**: Skill.
4. **Kamera**: auf den Bildschirm (Skill) und verstehen (Skill).
5. **Listen in Attributen**: ein allgemeines Werkzeug, bei Bedarf.

## 1. Verlauf und Logbuch

„Wann ging zuletzt die Haustür auf?“ – „Wie lange lief heute die Waschmaschine?“ – „War heute jemand im Keller?“

- Assist kann das nicht. HA hat Logbuch und History (REST `/api/logbook`, `/api/history/period`); roh sind das große Mengen.
- **Werkzeug `history`**:
  - Parameter: Entität (oder Name), Zeitraum (Standard: heute), Frage-Art.
  - Frage-Arten: `last_change` (letzter Wechsel in einen Zustand), `changes` (Wechsel im Zeitraum, höchstens N) und `duration` (wie lange in einem Zustand, z. B. „an“).
- **Ausgabe:** knapp, mit Uhrzeit in der Sprache des Gesprächs, etwa „Haustür: zuletzt geöffnet heute 14:12, 3-mal seit 6 Uhr“. Attribute gehen nicht mit.
- **Sichtbar** sind nur für Assist freigegebene Entitäten.
- **Für Aufgaben** als Ansicht `view: history` nutzbar.

## 2. Wetter-Vorhersage

„Brauche ich morgen einen Schirm?“ – „Wie wird das Wochenende?“

- `weather.get_forecasts` liefert stündliche oder tägliche Listen mit vielen Werten.
- **Werkzeug `weather_forecast`**: Parameter Zeitraum (heute, morgen, die nächsten Tage) und stündlich oder täglich.
- **Ausgabe je Tag:** Zustand, Tiefst- und Höchstwert, Regenwahrscheinlichkeit und -menge. Wind nur, wenn er auffällt. Stündlich nur die Stunden, in denen sich etwas ändert.
- **Welches Wetter:** die freigegebene Wetter-Entität; bei mehreren die des Hauses (Zone home) zuerst.

## 3. Musik über Music Assistant

„Spiel was von Norah Jones im Schlafzimmer“ – „Spiel meine Playlist Abendessen“ – „Spiel Radio Bob“ – „Spiel das auch in der Küche“

- Assists `play_media` kann wenig. Music Assistant kennt Künstler, Alben, Playlists und Radio und kann Räume gruppieren.
- **Skill `musik`:**
  - Felder: was (Künstler, Titel, Album, Playlist, Sender), Art (optional), Raum (Standard: der Satellit der Anfrage), Zufall (optional).
  - Abschluss: `music_assistant.play_media` am Media-Player des Raums.
  - Den Lautsprecher des Raums nimmt Mira über den Ort (Platzhalter `area`).
- **Gruppieren und Lautstärke:** „Auch in der Küche“ wird ein zweiter Abschluss (`media_player.join`). Lautstärke und Pause bleiben bei Assist.
- **Nur mit Music Assistant**; ohne MA gibt es den Skill nicht.

## 4. Kamera

**Auf den Bildschirm:** „Zeig mir die Haustür.“
- **Skill `kamera_zeigen`:**
  - Feld: Kamera.
  - Abschluss: die Aktion des Satelliten mit Bildschirm (Echo Show mit TECHO5: `esphome.<gerät>_home_show_camera` mit Dauer).
- Wie beim Wecker sagt Mira an Satelliten ohne Bildschirm, dass es dort nicht geht.

**Verstehen:** „Wer steht vor der Tür?“ – „Ist das Garagentor zu?“
- **Skill `kamera_fragen`:** Abschluss ist HAs `ai_task.generate_data` mit dem Kamerabild als Anhang und der Frage. Mira sagt das Ergebnis.
- Das braucht einen Abschluss, der eine **Antwort** zurückgibt (Aktion mit `return_response`). Das ist eine kleine Erweiterung der Skill-Aktionen: Mit `response: true` reicht der Abschluss die Antwort an das Modell weiter.
- **Datenschutz:** Das Bild geht an das Modell, das in `ai_task` eingestellt ist, möglicherweise in der Cloud. Die Anleitung sagt das.

## 5. Listen in Attributen

„Wann ist Strom heute am billigsten?“ – „Wann fährt der nächste Bus?“ – „Wo ist Diesel gerade günstig?“

- Viele Integrationen packen eine Liste in ein Attribut eines Sensors: Strompreise je Stunde, Abfahrten, Tankstellen, Sendungen. Der Zustand allein beantwortet die Frage nicht, die Liste roh ist zu viel.
- **Ein allgemeines Werkzeug `list_attribute`** statt eines Skills je Integration:
  - Parameter: Entität, Attribut, Felder, Sortierung (z. B. Preis aufsteigend) und Anzahl.
  - Zeiten in der Sprache des Gesprächs, nur die genannten Felder.
- **Für Aufgaben** als Ansicht nutzbar („Sag Bescheid, wenn Strom heute unter 20 Cent fällt“).

## Bewusst nicht

- **Alarmanlage und Schlösser** als Skill: Das geht über Assist, und es bleibt beim „erst nach Ja“.
- **Benachrichtigungen**: Das macht Mira selbst über die Kanäle der Person.
- **Sehr setup-spezifische Geräte** (Auto, Wallbox …) als mitgelieferte Skills: Die schreibt der Betreiber selbst, nach dem Muster der Skill-Aktionen. In der Anleitung kommt ein Beispiel dazu.
