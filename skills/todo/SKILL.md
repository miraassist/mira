---
name: todo
description: Etwas mit Fälligkeit oder Beschreibung auf eine To-do-Liste setzen („Setz Batterien auf die Einkaufsliste, bis Freitag“). Ohne Fälligkeit und Beschreibung reicht HassListAddItem.
license: MIT
metadata:
  mira:
    target:
      timeout: 10m
      fields:
        - name: item
          description: Der Eintrag, kurz, z. B. Batterien
        - name: list
          description: Die Liste (Name). Steht im Gedächtnis eine Standard-Liste der Person, nimm sie; sonst "standard"
          check: todo_list
        - name: due
          description: Fällig am, als YYYY-MM-DD
          optional: true
        - name: description
          description: Beschreibung, falls genannt
          optional: true
      finish:
        action: todo.add_item
        omit_empty: true
        data:
          entity_id: "{{ list }}"
          item: "{{ item }}"
          due_date: "{{ due }}"
          description: "{{ description }}"
---
# To-do mit Fälligkeit

- Rechne relative Angaben („bis Freitag“) mit dem heutigen Datum aus.
- Liste: genannt, sonst Standard aus dem Gedächtnis, sonst "standard". Fragt die Prüfung zurück, welche Liste, frag den Nutzer und biete danach an, dir den Standard zu merken.
- Nicht nachfragen, ob es passt: eintragen und kurz sagen, was auf welcher Liste steht, mit Fälligkeit.
