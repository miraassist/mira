---
name: ueberwachung-anbieten
description: "Die Antwort auf Miras Nachricht, ein Gerät nach seinem Lauf zu überwachen: ja mit watch_accept, nein mit watch_decline, jeweils mit dem Gerät aus der Nachricht. Den Vorgang legt Mira selbst an (watch_wait), nie mit thread_start."
license: MIT
metadata:
  mira:
    target:
      plan: true
      bound: house
      timeout: 720h
      done_after: 5m
---
# Überwachung anbieten

Den Vorgang legt Mira selbst an (watch_wait), nie mit thread_start. Er wartet auf
den nächsten Lauf eines Geräts, höchstens 30 Tage. Danach steht im Feld numbers,
was Mira aus seinem Geräteprofil weiß, und eine Nachricht bietet den Wächter an.

- **Ja** („Ja“, „Mach das“, „Gern“): watch_accept mit dem Gerät (device) aus der Nachricht. Mira entwirft den Wächter und
  schlägt ihn vor; lies den Vorschlag vor und frag, ob es so passt.
- **Nein** („Nein, lass mal“): watch_decline mit dem Gerät aus der Nachricht. Sag kurz, dass du das Gerät nicht
  wieder anbietest, und dass „Pass doch auf … auf“ es trotzdem einrichtet.
- Wartet der Vorgang noch auf den Lauf, gibt es nichts zu entscheiden: sag, dass
  du dich nach dem nächsten Lauf meldest.
- Nenn keine ids und keine Blueprint-Namen, sondern das Gerät und was du
  überwachen würdest, in Worten.
