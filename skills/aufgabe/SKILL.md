---
name: aufgabe
description: Eine dauerhafte Aufgabe einrichten, wenn der Nutzer möchte, dass Mira auf etwas achtet oder Bescheid sagt („Achte auf den Wasserverbrauch“, „Sag Bescheid, wenn …“). Daraus wird eine Home-Assistant-Automation.
license: MIT
metadata:
  mira:
    target:
      timeout: 15m
      confirm: true
      fields:
        - name: watch
          description: Was beobachtet wird, mit den Entitäten des Hauses (id oder Name)
          check: entities
        - name: when
          description: Wann reagiert wird, z. B. wenn es angeht, wenn er anschlägt, stündlich
        - name: measure
          description: Ab wann es zählt, z. B. Uhrzeit von–bis oder Grenzwert. "- " mit Grund, wenn der Wunsch nichts Relatives enthält („ungewöhnlich“, „zu viel“, „spät“ sind relativ)
        - name: reaction
          description: Was dann passiert – notice (fest melden), assess (Mira beurteilt jedes Mal selbst) oder switch (etwas schalten, nur wenn der Nutzer es will)
          enum: [notice, assess, switch]
      finish: tasks.automation
---
# Aufgabe einrichten

Aus dem Wunsch des Nutzers wird eine Automation in Home Assistant. Du sammelst, was sie braucht; den Entwurf macht Mira danach selbst und meldet sich mit einem Vorschlag.

- Trag mit thread_fill ein, was schon klar ist, auch aus dem Haus (Entitäten nachsehen statt fragen).
- Frag nur nach, was fehlt, kurz und mit einem Vorschlag: „Ab wann ist es für dich ungewöhnlich, etwa zwischen Mitternacht und 6 Uhr?“
- Relative Wünsche („ungewöhnlich“, „zu viel“, „spät“) brauchen einen Maßstab. Steht er in dem, was Mira über den Haushalt weiß, nimm ihn; sonst frag. Nur wenn der Nutzer ausdrücklich will, dass Mira es jedes Mal selbst beurteilt, ist reaction assess ohne festen Maßstab richtig.
- Fest melden (notice) ist richtig, wenn das Ereignis immer gesagt werden soll (Melder schlägt an, Licht geht an).
