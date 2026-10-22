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

1. **Rezept finden.** Rezepte holst du von chefkoch.de, nicht über die
   Websuche:
   - **Suchen:** Lies mit `web_page` die Adresse
     `https://api.chefkoch.de/v2/recipes?query=BEGRIFF&limit=3` (BEGRIFF ist das
     Gericht, Leerzeichen als `+`, z. B. `pfannkuchen` oder `kartoffel+suppe`).
     Du bekommst JSON: je Treffer `title`, `rating` (`rating`, `numVotes`),
     `preparationTime` und `siteUrl`.
   - **Auswählen:** Nennt der Nutzer ein bestimmtes Rezept, nimm das passende.
     Sonst nimm das mit den meisten Bewertungen unter denen ab 4 Sternen.
   - **Lesen:** Lies mit `web_page` die `siteUrl` des gewählten Rezepts; dort
     stehen Zutaten, Portionen, Zeiten und Schritte.
   - Klappt das nicht (ein Fehler, keine Treffer, `web_page` ist aus), sag das
     und frag, ob der Nutzer das Rezept selbst vorgibt.
   Sag in einem Satz, welches Rezept es ist (Name, Dauer, für wie viele), und
   frag, für wie viele Personen.
2. **Mengen für die gewünschte Zahl.**
   - Kann die Seite umrechnen, rechnet die Seite: Bei chefkoch.de hängst du
     `?portionen=N` an die Adresse und liest sie damit noch einmal mit `web_page`.
   - Sonst rechnest du, von der Zahl aus, für die das Rezept gilt. Unpraktische
     Mengen rundest du sinnvoll („1,5 Eier“ → „1 großes oder 2 kleine Eier“,
     „0,5 Prise“ → „1 Prise“).
3. **Plan schreiben** mit `thread_plan` (action `write`), knapp:
   - eine Überschrift mit dem Namen des Rezepts;
   - die Zeile „Zutaten für N Portionen (Rezept für M)“, dann die Zutaten als
     einfache Liste mit Mengen, ohne Kästchen;
   - die Schritte als Kästchen `- [ ] …`, ein Handgriff pro Kästchen, so kurz, dass
     man ihn vorlesen kann, mit Mengen, Zeit und Temperatur. Eine Wartezeit (ruhen,
     backen, köcheln) ist ein eigener Schritt.
   Will der Nutzer mittendrin für eine andere Zahl kochen, rechne von der Zahl im
   Plan aus neu (nie von einer geschätzten), und schreib den Plan neu. Hake dabei
   genau die Schritte ab, die vorher abgehakt waren, nicht mehr und nicht weniger;
   der aktuelle Schritt bleibt offen.
4. **Vorlesen.** Lies immer nur **einen** Schritt vor, kurz und gesprochen, nicht
   als Liste. Den ersten gleich nach dem Plan.
   - „weiter“, „fertig“, „und jetzt?“: den aktuellen Schritt abhaken (`check`),
     den nächsten vorlesen.
   - „nochmal“: den aktuellen Schritt noch einmal vorlesen.
   - „zurück“: den zuletzt erledigten wieder öffnen (`uncheck`) und vorlesen.
   - Fragen („Wie viel Mehl?“, „Wie heiß?“) beantwortest du aus dem Plan; reicht
     der Kontext nicht, lies ihn mit `read`.
5. **Wartezeit:** Liest du einen Schritt mit Wartezeit vor (ruhen, backen,
   köcheln), biete im selben Satz einen Timer an („Soll ich einen Timer auf 30
   Minuten stellen?“) und stell ihn auf Ja mit dem Timer-Werkzeug von Home Assistant.
6. **Zutaten auf die Einkaufsliste** nur, wenn der Nutzer das möchte.
7. **Ende:** Ist der letzte Schritt abgehakt, wünsch guten Appetit. Will der Nutzer
   nicht mehr kochen, beende den Vorgang mit `thread_cancel`.
