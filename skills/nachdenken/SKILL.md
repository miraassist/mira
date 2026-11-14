---
name: nachdenken
description: "Gibt eine Frage, die viel Wissen oder gründliches Nachdenken braucht, an ein stärkeres Modell und sagt dessen Antwort: Erklärungen („Erklär mir, wie eine Wärmepumpe funktioniert“, „Warum ist der Himmel blau?“), Vergleiche („Was ist der Unterschied zwischen Gas- und Induktionsherd?“), Abwägungen („Lohnt sich ein Balkonkraftwerk, was spricht dafür und dagegen?“). Nur, wenn kein anderer Skill dafür da ist; nicht für das Haus (Schalten, Zustände) und nicht für das, was andere Skills tun."
license: MIT
metadata:
  mira:
    agents:
      nachdenken:
        description: Beantwortet eine Wissensfrage, eine Erklärung, einen Vergleich oder eine Abwägung gründlich, mit dem stärksten Modell. Es kann nachschlagen, aber nichts verändern.
        # The strongest model: the one mira.yaml names for delegate
        # (agents.delegate), without an entry main (docs/spec/agents.md,
        # Decided 44).
        model: delegate
        context: background
        tools: read_only
        rights: [read_house, web]
        conversation: true
        instructions: >-
          Mira hat dir diese Frage gegeben und gibt deine Antwort an den Nutzer
          weiter. Antworte vollständig und sachlich; du darfst nachschlagen, aber
          nichts verändern.
---
# Nachdenken

Ein stärkeres Modell beantwortet Fragen, die viel Wissen oder gründliches
Nachdenken brauchen. Gib die Frage mit `nachdenken` ab:

- In `input` steht die Frage vollständig, mit allem Nötigen aus dem Gespräch
  (worauf sie sich bezieht, was der Nutzer schon gesagt hat): Das andere Modell
  kennt das Gespräch nicht.
- Fasse seine Antwort für den Nutzer im Stil des Kanals zusammen: am Lautsprecher
  kurz und in ganzen Sätzen, ohne Listen und Adressen; auf Telegram darf es
  ausführlicher sein.
- Sagt das Ergebnis, dass es länger braucht und als Nachricht kommt, sag dem
  Nutzer kurz, dass du dich mit der Antwort meldest.
- Fragt der Nutzer nach, gib die Nachfrage mit dem, was du schon weißt, wieder an
  `nachdenken`.
