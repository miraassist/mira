---
name: kochen
description: "Kochen und Backen nach Rezept, Schritt für Schritt vorgelesen („Lass uns Pfannkuchen backen“, „Ich will Lasagne kochen“); danach geht es mit „weiter“, „nochmal“, „zurück“."
license: MIT
metadata:
  mira:
    target:
      plan: true
      timeout: 1h
---
# Kochen

Du kochst oder backst mit dem Nutzer, Schritt für Schritt. Der Ablauf ist ein
Vorgang mit Plan: Der Plan überdauert das Gespräch, und im Kontext steht, wo ihr
seid.

1. **Rezept finden.** Nennt der Nutzer kein bestimmtes Rezept, such eins mit
   `web_search` (z. B. „Pfannkuchen Rezept chefkoch“) und lies das beste mit
   `web_page`. Ist die Websuche aus, sag das und frag, ob er das Rezept selbst
   vorgibt. Sag in einem Satz, welches Rezept es ist (Name, Dauer, für wie viele),
   und frag, für wie viele Personen; rechne die Mengen dann um.
2. **Plan schreiben** mit `thread_plan` (action `write`), knapp:
   - eine Überschrift mit dem Namen des Rezepts;
   - die Zutaten als einfache Liste mit Mengen für die gewünschte Zahl, ohne Kästchen;
   - die Schritte als Kästchen `- [ ] …`, ein Handgriff pro Kästchen, so kurz, dass
     man ihn vorlesen kann, mit Zeit und Temperatur. Eine Wartezeit (ruhen,
     backen, köcheln) ist ein eigener Schritt.
3. **Vorlesen.** Lies immer nur **einen** Schritt vor, kurz und gesprochen, nicht
   als Liste. Den ersten gleich nach dem Plan.
   - „weiter“, „fertig“, „und jetzt?“: den aktuellen Schritt abhaken (`check`),
     den nächsten vorlesen.
   - „nochmal“: den aktuellen Schritt noch einmal vorlesen.
   - „zurück“: den zuletzt erledigten wieder öffnen (`uncheck`) und vorlesen.
   - Fragen („Wie viel Mehl?“, „Wie heiß?“) beantwortest du aus dem Plan; reicht
     der Kontext nicht, lies ihn mit `read`.
4. **Wartezeit:** Liest du einen Schritt mit Wartezeit vor (ruhen, backen,
   köcheln), biete im selben Satz einen Timer an („Soll ich einen Timer auf 30
   Minuten stellen?“) und stell ihn auf Ja mit dem Timer-Werkzeug von Home Assistant.
5. **Zutaten auf die Einkaufsliste** nur, wenn der Nutzer das möchte.
6. **Ende:** Ist der letzte Schritt abgehakt, wünsch guten Appetit. Will der Nutzer
   nicht mehr kochen, beende den Vorgang mit `thread_cancel`.
