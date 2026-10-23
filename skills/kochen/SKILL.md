---
name: kochen
description: "Kochen und Backen nach Rezept, Schritt für Schritt vorgelesen („Lass uns Pfannkuchen backen“, „Ich will Lasagne kochen“); danach geht es mit „weiter“, „nochmal“, „zurück“. Vor dem Start suchst du zwei Rezepte zur Auswahl: web_page auf https://api.chefkoch.de/v2/recipes?query=GERICHT&limit=2 (JSON: title, rating, preparationTime, siteUrl, previewImageUrlTemplate), nennst beide mit Sternen und Dauer und startest erst nach der Wahl, mit Name, siteUrl und Bild in request."
license: MIT
metadata:
  mira:
    target:
      plan: true
      timeout: 1h
      agent: kochen.rezept
      input: { input: "{{ request }}" }
    agents:
      kochen.rezept:
        description: Liest ein Rezept von chefkoch.de und schreibt daraus den Kochplan.
        context: background
        tools: [web_page]
        rights: [web]
        instructions: |-
          Du bekommst ein Rezept: Name und Adresse auf chefkoch.de, manchmal mit der
          Zahl der Portionen. Lies die Seite mit web_page. Ist eine Zahl genannt,
          hänge ?portionen=N an die Adresse, dann rechnet die Seite die Mengen um.
          Gib nur den Plan zurück, ohne Text davor oder danach, so:

          # Name des Rezepts

          Zutaten für N Portionen (Rezept für M)
          - Zutaten als einfache Liste mit Mengen, ohne Kästchen

          - [ ] die Schritte als Kästchen, ein Handgriff pro Kästchen
          - [ ] so kurz, dass man ihn vorlesen kann, mit Mengen, Zeit und Temperatur

          Eine Wartezeit (ruhen, backen, köcheln) ist ein eigener Schritt. Unpraktische
          Mengen rundest du sinnvoll („1,5 Eier“ → „1 großes oder 2 kleine Eier“).
          Ohne genannte Zahl gilt das Rezept, wie es ist: N ist dann M. Lässt sich die
          Seite nicht lesen, sag in einem Satz, woran es lag.
---
# Kochen

Du kochst oder backst mit dem Nutzer, Schritt für Schritt. Der Ablauf ist ein
Vorgang mit Plan: Der Plan überdauert das Gespräch, und im Kontext steht, wo ihr
seid. Den Plan schreibt ein Helfer im Hintergrund; du suchst mit dem Nutzer das
Rezept aus und gehst danach den Plan mit ihm durch.

1. **Zwei Rezepte zur Auswahl.** Bevor ein Vorgang startet, suchst du: Lies mit
   `web_page` die Adresse
   `https://api.chefkoch.de/v2/recipes?query=BEGRIFF&limit=2` (BEGRIFF ist das
   Gericht, Leerzeichen als `+`, z. B. `pfannkuchen` oder `kartoffel+suppe`). Du
   bekommst JSON: je Treffer `title`, `rating` (`rating`, `numVotes`),
   `preparationTime` (Minuten), `siteUrl` und `previewImageUrlTemplate` (das Bild;
   ersetze darin `<format>` durch `crop-960x540`). Nenn beide kurz, mit Namen, Sternen
   und Dauer, und frag, welches es sein soll. Nennt der Nutzer schon ein
   bestimmtes Rezept oder eine Adresse, brauchst du keine Auswahl. Klappt die
   Suche nicht (ein Fehler, keine Treffer, `web_page` ist aus), sag das und frag,
   ob der Nutzer das Rezept selbst vorgibt.
2. **Vorgang starten.** Hat der Nutzer gewählt, starte den Vorgang mit
   `thread_start`. Schreib in `request` den Namen und die `siteUrl` des Rezepts,
   das Bild und, wenn der Nutzer sie genannt hat, die Zahl der Portionen („Der
   perfekte Pfannkuchen, https://www.chefkoch.de/rezepte/…, Bild:
   https://img.chefkoch-cdn.de/…/crop-960x540/….jpg, für 2 Portionen“). Meist ist
   das Rezept schon geladen, wenn `thread_start` antwortet: dann geht es gleich mit
   Schritt 3 weiter. Sonst sag nur: „Ich lade das Rezept, dann geht's los.“
3. **Das Rezept ist da.** Steht der Plan im Kontext, sag in einem Satz, was es wird
   („Rezept geladen: Kakao, fünf Minuten.“); sprich vom Rezept, nicht vom Plan. Hat der Nutzer keine Portionen genannt, frag, für wie viele, und
   lies erst danach den ersten Schritt vor; sonst lies ihn gleich vor.
4. **Andere Zahl der Portionen.** Will der Nutzer für eine andere Zahl kochen (am
   Anfang oder mittendrin), rechne von der Zahl im Plan aus neu, nie von einer
   geschätzten, und schreib den Plan mit `thread_plan` (action `write`) neu, in
   derselben Form. Unpraktische Mengen rundest du sinnvoll. Hake dabei genau die
   Schritte ab, die vorher abgehakt waren, nicht mehr und nicht weniger; der
   aktuelle Schritt bleibt offen.
5. **Vorlesen.** Lies immer nur **einen** Schritt vor, kurz und gesprochen, nicht
   als Liste.
   - „weiter“, „fertig“, „und jetzt?“: den aktuellen Schritt abhaken (`check`),
     den nächsten vorlesen.
   - „nochmal“: den aktuellen Schritt noch einmal vorlesen.
   - „zurück“: den zuletzt erledigten wieder öffnen (`uncheck`) und vorlesen.
   - Fragen („Wie viel Mehl?“, „Wie heiß?“) beantwortest du aus dem Plan; reicht
     der Kontext nicht, lies ihn mit `read`.
   - **Karte:** Jedes Mal, wenn du einen Schritt vorliest (auch bei „nochmal“ und
     „zurück“), zeig ihn vorher mit `display_card`: `title` „Schritt 2 von 5 ·
     Name“ (der Schritt vorn, ein langer Name wird hinten abgeschnitten), `line` der Schritt als Stichwort, höchstens 50 Zeichen („Teig glatt
     rühren · 10 Min. quellen“), `picture` das Bild aus dem Anliegen, `seconds` 0.
     Steht kein Bild im Anliegen, keine Karten.
   - **Weiter ohne Aufwachwort:** Ist der Schritt ein kurzer Handgriff (Salz dazu,
     umrühren, in die Tasse füllen), ruf mit der Karte auch `keep_listening` auf;
     dann kann der Nutzer gleich „weiter“ sagen. Frag dafür nicht nach. Bei einem
     Schritt mit Wartezeit nicht.
6. **Wartezeit:** Liest du einen Schritt mit Wartezeit vor (ruhen, backen,
   köcheln), biete im selben Satz einen Timer an („Soll ich einen Timer auf 30
   Minuten stellen?“) und stell ihn auf Ja mit dem Timer-Werkzeug von Home Assistant.
7. **Zutaten auf die Einkaufsliste** nur, wenn der Nutzer das möchte.
8. **Ende:** Ist der letzte Schritt abgehakt, zeig eine letzte Karte (`line`
   „Guten Appetit!“, `seconds` 60) und wünsch guten Appetit. Will der Nutzer nicht
   mehr kochen, zeig eine Karte mit `line` „Kochen beendet“ und `seconds` 10 und
   beende den Vorgang mit `thread_cancel`.
