---
name: aufgabe
description: Eine dauerhafte Aufgabe einrichten, wenn der Nutzer möchte, dass Mira auf etwas achtet oder Bescheid sagt („Achte auf den Wasserverbrauch“, „Sag Bescheid, wenn …“), auch für ganze Gruppen („wenn irgendwo Licht ewig an ist“, „wenn ein Fenster offen ist und keiner da ist“). Daraus wird eine Home-Assistant-Automation. Auch, wenn er fragt, welche Geräte Mira überwachen könnte („Gibt's Geräte, die du überwachen könntest?“), oder etwas nicht oder wieder überwacht haben will („Überwach die Terrasse wieder“).
license: MIT
metadata:
  mira:
    chapter: "## Mira passt aufs Haus auf"
    target:
      timeout: 15m
      confirm: true
      fields:
        - name: watch
          description: "Was beobachtet wird, mit den Entitäten des Hauses (id oder Name); gilt der Wunsch einer ganzen Gruppe, „Gruppe:“ und welche, z. B. „Gruppe: alle Lichter außer Terrasse“, „Gruppe: Fenster und Türen im Dachgeschoss“, „Gruppe: Bewegungsmelder im Garten“, „Gruppe: Temperaturen im Keller“, „Gruppe: Fenster Gast“ (ein Gruppen-Helfer)"
          check: entities
        - name: when
          description: Wann reagiert wird, z. B. wenn es angeht, wenn er anschlägt, stündlich
        - name: measure
          description: Ab wann es zählt, z. B. Uhrzeit von–bis oder Grenzwert. "- " mit Grund, wenn der Wunsch nichts Relatives enthält („ungewöhnlich“, „zu viel“, „spät“ sind relativ)
        - name: reaction
          description: Was dann passiert – message (fest melden), assess (Mira beurteilt jedes Mal selbst) oder switch (etwas schalten, nur wenn der Nutzer es will)
          enum: [message, assess, switch]
      finish: tasks.automation
---
# Aufgabe einrichten

Aus dem Wunsch des Nutzers wird eine Automation in Home Assistant. Du sammelst, was sie braucht; den Entwurf macht Mira danach selbst und meldet sich mit einem Vorschlag.

- Trag mit thread_fill ein, was schon klar ist, auch aus dem Haus (Entitäten nachsehen statt fragen).
- Frag nur nach, was fehlt, kurz und mit einem Vorschlag: „Ab wann ist es für dich ungewöhnlich, etwa zwischen Mitternacht und 6 Uhr?“
- Relative Wünsche („ungewöhnlich“, „zu viel“, „spät“) brauchen einen Maßstab. Steht er in dem, was Mira über den Haushalt weiß, nimm ihn; sonst frag. Nur wenn der Nutzer ausdrücklich will, dass Mira es jedes Mal selbst beurteilt, ist reaction assess ohne festen Maßstab richtig.
- Fest melden (message) ist richtig, wenn das Ereignis immer gesagt werden soll (Melder schlägt an, Licht geht an).
- Für manches hat Mira fertige Wächter (Blueprints in Home Assistant): Wasser, das ohne Pause läuft („Pass auf, ob bei uns Wasser dauernd läuft“), einen Wasserzähler, der nie stillsteht (tropfender Hahn, Spülung läuft nach), ein Gerät, das jeden Tag zu festen Zeiten läuft (Aquariumlicht, Pumpe), ein Gerät im Takt (Kühlschrank, Gefriertruhe: läuft zu lange ohne Pause oder gar nicht mehr) und einen Gerätelauf (Spülmaschine, Waschmaschine: läuft länger als sonst oder heizt nicht auf). Du sammelst dafür dasselbe wie für jede Aufgabe; den passenden Blueprint nimmt Mira beim Entwurf selbst. Was keiner abdeckt, wird wie bisher eine eigene Automation.
- Ein Gerät, das jeden Tag zu festen Zeiten läuft („Achte aufs Aquarium“): ruf device_profile mit seinem Leistungssensor auf und schlag die Zeiten daraus in Worten vor, mit der üblichen Leistung („Das Aquariumlicht läuft jeden Tag von 7:40 bis 11:40 und von 14:20 bis 21:00, mit rund 50 Watt. Soll ich mich melden, wenn es davon abweicht?“). Korrigiert der Nutzer eine Zeit, nimm seine. Die bestätigten Zeiten trägst du in measure ein, als Uhrzeiten von–bis wie „07:40-11:40, 14:20-21:00“. Findet das Profil keinen Tagesablauf oder zu wenige Tage, sag das und frag nach den Zeiten, bevor du etwas einträgst; nenn keine Zeiten, die nicht im Profil stehen, und mach daraus keine Aufgabe, die selbst beurteilt (assess).
- Ein Gerät im Takt oder mit Geräteläufen („Pass auf den Kühlschrank auf“, „Achte auf die Spülmaschine“): ruf device_profile auf. Reicht es für einen Wächter, schlag in Worten vor, wann du dich meldest, mit Zahlen über dem, was das Profil als normal zeigt (beim Kühlschrank: länger als sein längster Lauf und seine längste Pause; bei der Spülmaschine: länger als ihr längster Lauf, Heizen unter ihrer niedrigsten Spitze), und trag die bestätigten Zahlen in measure ein. Fehlen Läufe, ruf watch_wait mit dem Leistungssensor auf, statt eine Aufgabe zu beginnen (einen schon begonnenen Vorgang brichst du mit thread_cancel ab), und sag: „Die Spülmaschine nehm ich in die Überwachung, wenn sie das nächste Mal gelaufen ist.“ Frag nie nach Zahlen, die der Nutzer nicht kennt.
- Fragt der Nutzer, welche Geräte du überwachen könntest („Gibt's Geräte, die du überwachen könntest?“), ruf watch_candidates auf und nenn die Kandidaten kurz in Worten, jeden mit dem, was du an ihm überwachen würdest (ohne ids und Blueprint-Namen). Will er einen davon, richte ihn wie jede Aufgabe ein. Ist nichts Neues dabei, sag, dass du nichts Neues zum Überwachen findest. Nenn auch, welche Geräte du nicht lesen konntest.
- Wächter für Gruppen: Gilt der Wunsch allen Lichtern, Fenstern und Türen, Bewegungsmeldern oder Temperaturen, ganz oder in Bereichen oder Etagen („Sag Bescheid, wenn irgendwo Licht ewig an ist“, „wenn ein Fenster offen ist und keiner da ist“, „Melde Bewegung im Garten nachts“, „wenn der Keller unter 5 Grad fällt“), trag in watch „Gruppe:“ und die Auswahl in Worten ein, nicht die einzelnen Entitäten. Was der Nutzer ausnimmt („die Terrasse nicht“), gehört mit hinein; den Entwurf und welche Lampen draußen sind, macht Mira selbst. Fehlt eine Zahl, nimm, was der Nutzer sagt, sonst lass sie weg (Mira nimmt den Standard und sagt ihn im Vorschlag). Nur „nachts“ braucht Uhrzeiten: frag „Von wann bis wann ist nachts?“, wenn er sie nicht sagt. Was im Vorschlag als ausgenommen steht, bekommt beim Ja das Label „Mira: nicht überwachen“ und ist dann in keinem Wächter.
- Nennt der Nutzer eine einzelne Lampe, ein Fenster oder einen Sensor, die nicht mehr oder wieder überwacht werden sollen („Überwach die Terrasse nicht mehr“, „Überwach die Terrasse wieder“), ist das eine Ausnahme, kein Wächter: ruf watch_exception mit ihr auf; die Wächter bleiben, wie sie sind. task_delete nur, wenn er einen ganzen Wächter nicht mehr will („Pass nicht mehr auf die Fenster auf“).
- Ein Wächter ist eine Aufgabe wie jede andere: „Pass erstmal nicht aufs Wasser auf“ schaltet ihn mit task_enable aus, „Pass wieder aufs Wasser auf“ an; „Pass nicht mehr aufs Wasser auf“ löscht ihn mit task_delete.
