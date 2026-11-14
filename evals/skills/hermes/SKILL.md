---
name: hermes
description: "Lässt Hermes, einen Recherche-Agenten auf einem anderen Gerät, gründlich recherchieren: er durchsucht über längere Zeit viele Quellen (Testberichte, Herstellerangaben, Foren, Preise), vergleicht Produkte und Angebote und schreibt eine Übersicht mit Empfehlung – mehr, als Mira in einem Gespräch nachschlagen kann. Für Aufträge wie „Lass Hermes recherchieren …“, „Finde heraus, welche … passt“, „Such mir die besten … raus“."
metadata:
  mira:
    # An eval fixture (docs/spec/evals.md, Remote agents and delegate): the
    # fake A2A agent's container of testenv/compose.yaml on this machine, its
    # default token, and its skill "ask", which asks back at once.
    agents:
      recherche:
        remote:
          url: "http://127.0.0.1:8791"
          token: "fake-a2a-token"
          a2a_skill: ask
        name: Hermes
    target:
      timeout: 30m
      fields:
        - name: fact
          description: Die Übersicht, die Hermes geschrieben hat.
          agent: recherche
          input: { input: "{{ request }}" }
      finish: memory.save
---
# Hermes

Starte den Vorgang mit thread_start; die Frage des Nutzers ist die Anfrage. Hermes arbeitet daran. Steht sein Ergebnis schon in der Antwort von thread_start, sag es dem Nutzer. Sonst sag kurz, dass Hermes daran arbeitet und du dich meldest. Fragt Hermes nach, stell dem Nutzer die Frage und gib seine Antwort mit agent_answer weiter.
