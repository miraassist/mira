---
name: regel
description: "Eigene Regeln, die festlegen, was Mira tun darf und was nicht („Merk dir eine Regel: …“, „Ab jetzt sollst du nie …“, „Wenn du nicht weißt, wer spricht, schließt du nichts auf“); außerdem „Welche Regeln gibt es?“, eine Regel aus- oder wieder einschalten („Heute Abend gilt die Regel mit der Musik nicht“) oder löschen."
license: MIT
metadata:
  mira:
    target:
      timeout: 15m
      fields:
        - name: wunsch
          description: Was die Regel festlegen soll, in den Worten des Nutzers und vollständig (wer, was, wann, welches Gerät oder welcher Raum)
        - name: regel
          description: wie die Regel genau lauten soll (ein Helfer schreibt sie aus dem Wunsch; trag hier selbst nie etwas ein)
          agent: regel.schreiben
          input: { input: "{{ wunsch }}" }
          check: hook.check
      finish: hook.save
    agents:
      regel.schreiben:
        description: Schreibt aus dem Wunsch des Nutzers eine Regel für Mira (einen Hook in Starlark) und prüft, dass sie lädt.
        context: background
        model: delegate
        tools: [hook_reference, hook_try]
        rights: [read_house]
        max_rounds: 8
        instructions: |-
          Du schreibst eine Regel für Mira: einen Hook in Starlark, der vor jedem Werkzeug
          entscheidet, ob Mira es benutzen darf. Du bekommst den Wunsch des Nutzers.

          1. Ruf zuerst hook_reference auf. Dort steht, wie eine Regel aussieht, was sie
             sieht, und welche Bereiche, Personen und Entitäten es in diesem Haus gibt.
          2. Schreib die Regel. Nimm IDs, keine Namen: Personen als Schlüssel, Bereiche
             als ID, Labels als ID. Mach sie genau so streng wie gewünscht, nicht strenger.
             Code und Kommentare auf Englisch; DESCRIPTION und der Text in deny() in der
             Sprache des Wunsches, kurz und so, dass Mira ihn sagen kann.
          3. Prüf sie mit hook_try und verbessere sie, bis hook_try OK sagt, höchstens
             dreimal.

          Gib nur ein JSON-Objekt zurück, ohne Text davor oder danach:
          {"file": "kurzer_englischer_name.star", "summary": "ein Satz in der Sprache des
          Wunsches, was die Regel tut", "code": "der vollständige Code"}

          Eine Regel kann nur verhindern oder nachfragen lassen, nichts selbst tun. Verlangt
          der Wunsch etwas anderes (etwas zu schalten, zu erinnern), oder bleibt hook_try
          beim Fehler, gib {"error": "ein Satz, warum"} zurück.
---
# Regeln

Regeln legen fest, was du tun darfst und was nicht. Sie gelten für alles, was du
mit Werkzeugen tust, und sie können dich nur strenger machen.

1. **Eine neue Regel.** Starte einen Vorgang mit `thread_start` und trag den
   Wunsch als `wunsch` ein, vollständig und in den Worten des Nutzers. Ist er
   unklar (welches Gerät, welche Person, ab wann), frag kurz nach, bevor du ihn
   einträgst. Das Feld `regel` füllt ein Helfer; trag dort nie selbst etwas ein.
   Der Helfer braucht eine Weile: Sag nur kurz, dass du die Regel schreibst und
   dich gleich meldest. Den Vorschlag formulierst du nie selbst. Er kommt vom
   Vorgang (im Kontext oder als seine Nachricht): ein Satz, was die Regel tut,
   und die Frage, ob du sie einschalten sollst. Gib ihn so weiter. Nach dem Ja
   bestätigst du mit `thread_confirm`; sag dann, dass sie aktiv ist, aber nicht
   getestet, und dass der Nutzer sie am besten einmal ausprobiert.
2. **Konnte der Helfer die Regel nicht schreiben,** sag das und frag, wie der
   Nutzer sie meint. Trag die neue Fassung als `wunsch` ein, dann schreibt der
   Helfer sie neu.
3. **Welche Regeln gibt es?** `hook_list`. Nenn sie kurz, mit an oder aus.
4. **Eine Regel aus- oder einschalten:** `hook_switch` mit dem Namen aus
   `hook_list`. Ausschalten löscht nichts; sie lässt sich wieder einschalten.
5. **Eine Regel löschen:** `hook_delete`. Frag vorher, ob ausschalten nicht reicht.

Regeln ändern geht nur für jemanden, den du kennst, und immer mit Ja. Deine eigenen
Regeln (etwa „Schlösser nur mit Ja“) lassen sich nicht ändern.
