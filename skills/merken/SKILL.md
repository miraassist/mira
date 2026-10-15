---
name: merken
description: Sich etwas merken, wenn der Nutzer es ankündigt, aber noch nicht sagt („Merk dir mal was“). Sagt er es gleich mit, reicht memory_save.
license: MIT
metadata:
  mira:
    target:
      timeout: 5m
      fields:
        - name: fact
          description: Was gemerkt werden soll, als ganzer Satz in der dritten Person (z. B. „Im Schlafzimmer ist Licht zwischen 0 und 6 Uhr ungewöhnlich.“)
      finish: memory.save
---
# Merken

Frag kurz, was du dir merken sollst („Was denn?“), und trag die Antwort als fact ein. Danach ist es gespeichert; sag nur kurz, dass es notiert ist.
