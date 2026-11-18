---
name: flug
description: "Einen Flug im Auge behalten: wann er landet, ob er Verspätung hat, und eine Nachricht, wenn er gelandet ist („Wann landet LH 123?“, „Behalt Omas Flug LH 123 im Auge“, „Ist der Flug aus Brüssel schon gelandet?“). Braucht in Home Assistant die Integration Flightradar24 (HACS)."
license: MIT
metadata:
  mira:
    automations:
      - id: gelandet
        blueprint: gelandet.yaml
        alias: "Mira: verfolgter Flug gelandet"
    scripts:
      - id: abfrage
        blueprint: flug.yaml
        alias: "Mira Flug"
        description: >-
          Verfolgt einen Flug mit Flightradar24, sagt seinen Stand oder hört damit auf.
          Felder: aktion (verfolgen, stand, vergessen) und flugnummer.
---
# Flug

Du verfolgst Flüge mit dem Werkzeug `flug__abfrage` (das Skript „Mira Flug“ in
Home Assistant; Mira legt es selbst an).
Es nimmt `aktion` und `flugnummer` und gibt die verfolgten Flüge zurück, jeden mit
von, nach, stand (noch keine Daten, noch nicht gestartet, in der Luft, gelandet),
abflug_geplant, abflug, ankunft_geplant, ankunft_erwartet, verspaetung_minuten,
ankunft und terminal (Zeiten von heute als „heute 20:15“, andere mit Wochentag
und Datum: „Do 19.11. 06:40“; sag sie so, „heute“ heißt heute); dazu `verfolgt`: ob die genannte Flugnummer jetzt verfolgt
wird, und `schon_gelandet`: ob der Flug beim Verfolgen schon gelandet war.

Die Flugnummer gibst du ohne Leerzeichen und groß („LH 123“ → LH123).

- **Verfolgen** („Behalt Omas Flug LH 123 im Auge“, und auch bei „Wann landet
  LH 123?“, wenn er noch nicht verfolgt wird): `aktion: verfolgen`. Ist danach
  `schon_gelandet` wahr, ist der Flug schon gelandet und wird nicht verfolgt: Sag
  das immer mit der tatsächlichen Ankunft (ankunft), dem Terminal und der
  Verspätung oder wie viel früher er kam (verspaetung_minuten), und dass du dazu
  keine Nachricht mehr schickst („LH 1962 ist schon um 19:49 gelandet, fünf
  Minuten früher als geplant, Terminal 1.“). Ist sonst `verfolgt` falsch, kennt Flightradar24 die
  Nummer gerade nicht: Sag das und frag, ob die Nummer stimmt. Ist `verfolgt` wahr, sag, dass du Bescheid gibst,
  wenn er gelandet ist, und nenn den Stand.
- **Stand** („Wann landet er?“, „Hat der Flug Verspätung?“, „Welche Flüge
  verfolgst du?“): `aktion: stand`, mit der Flugnummer oder leer für alle. Nenn,
  was die Antwort sagt: von wo nach wo, den Stand und die erwartete Ankunft
  (ankunft_erwartet). Ist verspaetung_minuten größer als 5, sag die Verspätung
  immer dazu, auch ungefragt („16 Minuten später als geplant“); ist sie kleiner
  als -5, dass er früher kommt. Nach der Landung die tatsächliche Ankunft (ankunft)
  und das Terminal.
- **Vergessen** („Du kannst den Flug vergessen“, auch kurz „Und LH 456 auch“
  nach einem Vergessen): `aktion: vergessen`. Sag danach klar, dass du den Flug
  nicht mehr im Auge behältst („LH 123 verfolge ich nicht mehr.“), nie, dass du ihn
  im Auge behältst.

Flightradar24 kennt zu einer Flugnummer den Flug, der gerade fliegt, oder den
nächsten nach Plan; ein Datum kann man nicht angeben. Steht bei stand „noch keine
Daten“, hat Flightradar24 noch keine Zeiten: Sag das; die Zeiten kommen, wenn der
Flug näher rückt. Ein Gate kennt Flightradar24 nicht, nur das Terminal.

Nenne nur Zeiten, die das Skript zurückgibt; rechne keine dazu und rate keine.
Gibt es das Werkzeug nicht oder antwortet es mit einem Fehler, sag, dass dafür in
Home Assistant die Integration Flightradar24 nötig ist (steht in Miras Anleitung);
das Skript legt Mira beim Start selbst an.

Ist ein verfolgter Flug gelandet, bekommt das Haus von selbst eine Nachricht, und
der Flug wird nicht mehr verfolgt; fragt danach jemand, sag, dass er gelandet ist,
wie es die Nachricht gesagt hat.
