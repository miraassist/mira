# language: de
#
# Prüfbare Regeln zu docs/spec/agenten.md. Jedes Szenario hat eine feste Nummer (AG-nn), die nie
# wiederverwendet wird; der Go-Test dazu trägt sie im Namen (TestAG07_…). Die Tags @schritt1 … @schritt8
# sagen, in welchem Schritt der Umstellung (agenten.md, „Umstellung in Schritten“) das Szenario gebaut wird.
#
# Gruppen: AG-0x Rechte · AG-1x Tiefe · AG-2x synchron · AG-3x asynchron und Lauf-Datensatz ·
#          AG-4x Zustellen · AG-5x Protokoll · AG-6x Aufruf aus Home Assistant und als Werkzeug · AG-7x Modell

Funktionalität: AG Agenten

  # --- Rechte (agenten.md: Rechte) -------------------------------------------------------------------

  @schritt1
  Szenario: AG-01 Wirksam ist, was Aufruf-Kontext und Agent beide erlauben
    Angenommen ein Agent, dessen Beschreibung die Rechte „Haus lesen“ und „Netz“ nennt
    Und ein Aufruf-Kontext mit den Rechten „Haus lesen“ und „Haus schalten“
    Wenn der Agent mit diesem Aufruf-Kontext läuft
    Dann sind seine wirksamen Rechte nur „Haus lesen“

  @schritt1
  Szenario: AG-02 Ohne das Recht „Haus schalten“ wird eine schaltende Aktion abgelehnt
    Angenommen ein Lauf ohne das wirksame Recht „Haus schalten“
    Wenn das Modell eine Aktion aufruft, die in Home Assistant etwas ändert
    Dann wird die Aktion nicht ausgeführt
    Und das Modell bekommt als Ergebnis, dass dieser Lauf nicht schalten darf

  @schritt1
  Szenario: AG-03 Ein Unter-Agent bekommt höchstens die wirksamen Rechte seines Aufrufers
    Angenommen ein Lauf mit den wirksamen Rechten „Haus lesen“ und „Netz“
    Und ein zweiter Agent, dessen Beschreibung alle Rechte nennt
    Wenn der Lauf den zweiten Agenten ruft
    Dann enthält dessen Aufruf-Kontext höchstens „Haus lesen“ und „Netz“

  @schritt1
  Szenario: AG-04 Wer nicht schalten darf, kann auch über einen anderen Agenten nicht schalten
    Angenommen ein Lauf ohne das wirksame Recht „Haus schalten“
    Und ein zweiter Agent, der Aktionen in Home Assistant aufrufen kann
    Wenn der Lauf den zweiten Agenten ruft und dessen Modell eine schaltende Aktion aufruft
    Dann wird die Aktion nicht ausgeführt

  @schritt1
  Szenario: AG-05 Rechte ersetzen den Wächter nicht
    Angenommen ein Lauf mit dem wirksamen Recht „Haus schalten“
    Wenn das Modell eine kritische Aktion aufruft (etwa ein Schloss öffnen)
    Dann fragt der Wächter wie bisher nach, bevor sie ausgeführt wird

  @schritt1
  Szenario: AG-06 Fremder Text eines Unter-Agenten gilt für den Aufrufer
    Angenommen ein Lauf, in dem noch kein fremder Text im Spiel ist
    Wenn er einen Unter-Agenten ruft, der eine Webseite liest
    Und der Lauf danach etwas behalten oder weitergeben will (Merken, Nachricht)
    Dann fragt Mira vorher nach, wie nach Web-Inhalt im eigenen Lauf

  @schritt1
  Szenario: AG-07 Nach Web-Inhalt bleiben kritische Aktionen gesperrt
    Angenommen ein Lauf, der Web-Inhalt gelesen hat
    Wenn das Modell danach eine kritische Aktion aufruft
    Dann wird sie nicht ausgeführt, auch nicht mit einem Ja

  # --- Tiefe (agenten.md: Tiefe) ---------------------------------------------------------------------

  @schritt1
  Szenario: AG-10 Ein äußerer Aufruf hat Tiefe 0, jeder Unter-Agent eine mehr
    Wenn Home Assistant einen Agenten ruft
    Dann hat dessen Aufruf-Kontext die Tiefe 0
    Und ein Agent, den er ruft, hat die Tiefe 1

  @schritt1
  Szenario: AG-11 Ein Aufruf über die erlaubte Tiefe hinaus scheitert
    Angenommen die Einstellung agents.max_depth ist nicht gesetzt
    Und ein Lauf mit der Tiefe 3
    Wenn er einen weiteren Agenten ruft
    Dann scheitert der Aufruf
    Und der Grund ist, dass die erlaubte Tiefe erreicht ist

  @schritt1
  Szenario: AG-12 Die erlaubte Tiefe ist einstellbar
    Angenommen agents.max_depth ist 1
    Und ein Lauf mit der Tiefe 1
    Wenn er einen weiteren Agenten ruft
    Dann scheitert der Aufruf

  # --- Synchron (agenten.md: Synchron und asynchron) -------------------------------------------------

  @schritt1
  Szenario: AG-20 Synchron bekommt der Aufrufer die Antwort
    Wenn ein Agent synchron mit einem Timeout gerufen wird
    Und er vor dem Timeout fertig ist
    Dann bekommt der Aufrufer seine Antwort

  @schritt1
  Szenario: AG-21 Timeout, und der Aufrufer will abbrechen
    Angenommen ein synchroner Aufruf, dessen Aufrufer beim Timeout abbrechen will
    Wenn der Agent bis zum Timeout nicht fertig ist
    Dann wird der Lauf abgebrochen
    Und der Aufrufer bekommt, dass die Zeit abgelaufen ist

  @schritt6
  Szenario: AG-22 Timeout, und der Aufrufer lässt weiterlaufen
    Angenommen ein synchroner Aufruf, dessen Aufrufer beim Timeout weiterlaufen lassen will
    Wenn der Agent bis zum Timeout nicht fertig ist
    Dann bekommt der Aufrufer die Aufruf-ID
    Und der Lauf läuft weiter und wird wie ein asynchroner zugestellt

  @schritt1
  Szenario: AG-23 Eine Ausgabe, die nicht dem Schema entspricht, ist kein Ergebnis
    Angenommen ein Agent mit einem Schema für die Ausgabe
    Wenn das Modell eine Antwort gibt, die dem Schema nicht entspricht
    Dann gilt der Lauf als fehlgeschlagen

  @schritt1
  Szenario: AG-24 Ein hängender Modellaufruf wird nach den Grenzen wiederholt
    Angenommen ein Agent mit den Grenzen „60 s je Versuch, einmal wiederholen“
    Wenn der erste Modellaufruf nach 60 s keine Antwort hat
    Dann wird er einmal wiederholt
    Und mit der Antwort des zweiten Versuchs geht der Lauf weiter

  # --- Asynchron und Lauf-Datensatz (agenten.md: Der Lauf-Datensatz) --------------------------------

  @schritt6
  Szenario: AG-30 Asynchron kommt sofort die Aufruf-ID
    Wenn ein Agent asynchron gerufen wird
    Dann bekommt der Aufrufer sofort eine Aufruf-ID
    Und der Lauf-Datensatz hat den Status „läuft“

  @schritt6
  Szenario: AG-31 Ein fertiger Lauf hält seine Ausgabe
    Angenommen ein asynchroner Lauf eines Agenten mit einem Schema für die Ausgabe
    Wenn der Lauf fertig ist
    Dann hat der Lauf-Datensatz den Status „fertig“
    Und er enthält die Ausgabe nach dem Schema

  @schritt6
  Szenario: AG-32 Ein gescheiterter Lauf hält seinen Fehler
    Angenommen ein asynchroner Lauf
    Wenn der Lauf scheitert
    Dann hat der Lauf-Datensatz den Status „fehlgeschlagen“
    Und er enthält den Fehler

  @schritt6
  Szenario: AG-33 Ein Lauf über seine Zeitgrenzen hinaus ist abgelaufen
    Angenommen ein asynchroner Lauf
    Wenn er seine Zeitgrenzen überschreitet
    Dann hat der Lauf-Datensatz den Status „abgelaufen“

  @schritt6
  Szenario: AG-34 Der Lauf-Datensatz kennt seinen Aufrufer
    Wenn ein Lauf einen anderen Agenten asynchron ruft
    Dann enthält dessen Lauf-Datensatz die Aufruf-ID des aufrufenden Laufs

  @schritt6
  Szenario: AG-35 Über die Aufruf-ID lässt sich ein Lauf abfragen
    Angenommen ein asynchroner Lauf mit seiner Aufruf-ID
    Wenn der Aufrufer mit der Aufruf-ID nachfragt
    Dann bekommt er den Lauf-Datensatz mit Status und, falls fertig, der Ausgabe

  @schritt6
  Szenario: AG-36 60 Minuten nach seinem Ende ist ein Lauf-Datensatz weg
    Angenommen ein Lauf, der vor 61 Minuten fertig wurde
    Wenn jemand mit seiner Aufruf-ID nachfragt
    Dann gibt es den Lauf nicht mehr

  # --- Zustellen (agenten.md: Zustellen) -------------------------------------------------------------

  @schritt6
  Szenario: AG-40 Zustellen als Home-Assistant-Ereignis
    Angenommen ein asynchroner Aufruf mit dem Zustellweg „HA-Ereignis“
    Wenn der Lauf fertig ist
    Dann feuert Home Assistant das Ereignis mira_agent_done
    Und es enthält Aufruf-ID, Agent, Status und Ausgabe

  @schritt6
  Szenario: AG-41 Zustellen als Nachricht
    Angenommen ein asynchroner Aufruf mit dem Zustellweg „Nachricht an eine Person“
    Wenn der Lauf fertig ist
    Dann bekommt die Person eine Nachricht, die das Gespräch aus dem Ergebnis formuliert hat

  @schritt7
  Szenario: AG-42 Zustellen als Fortsetzung
    Angenommen ein Lauf, der einen anderen Agenten asynchron mit dem Zustellweg „Fortsetzung“ ruft
    Wenn der gerufene Lauf fertig ist
    Dann geht der aufrufende Lauf mit dessen Ergebnis weiter

  # --- Protokoll (agenten.md: Protokoll) -------------------------------------------------------------

  @schritt1
  Szenario: AG-50 Jeder Lauf schreibt eine Zeile „agent done“
    Wenn ein Lauf endet, gleich wie
    Dann steht im Log eine Zeile „agent done“
    Und sie nennt Agent, Aufruf-ID, Tiefe, Herkunft, Modus, Status, Modell, Dauer, Modellaufrufe und Tokens

  @schritt1
  Szenario: AG-51 Unter-Agenten lassen sich ihrem Aufrufer zuordnen
    Wenn ein Lauf einen anderen Agenten ruft
    Dann nennt die Zeile „agent done“ des gerufenen Laufs die Aufruf-ID des aufrufenden Laufs und seine Tiefe

  # --- Aufruf aus Home Assistant und als Werkzeug (agenten.md: Aufrufen) -----------------------------

  @schritt4
  Szenario: AG-60 Ein Agent in der Werkzeugliste ist ein Werkzeug
    Angenommen ein Agent, in dessen Werkzeugliste ein zweiter Agent steht
    Wenn sein Modell gerufen wird
    Dann sieht es den zweiten Agenten als Werkzeug, mit Name, Beschreibung und Eingabe-Schema

  @schritt4
  Szenario: AG-61 Ein Agent, der nicht in der Werkzeugliste steht, ist kein Werkzeug
    Angenommen ein Agent, in dessen Werkzeugliste ein zweiter Agent nicht steht
    Wenn sein Modell gerufen wird
    Dann sieht es den zweiten Agenten nicht

  @schritt6
  Szenario: AG-62 Aus Home Assistant synchron mit Antwort
    Wenn eine Automation die Aktion mira.agent synchron mit einem Agenten und seiner Eingabe aufruft
    Dann bekommt die Automation die Antwort des Agenten in ihrer response_variable

  @schritt6
  Szenario: AG-63 Aus Home Assistant asynchron
    Wenn eine Automation die Aktion mira.agent asynchron aufruft
    Dann bekommt sie die Aufruf-ID
    Und das Ergebnis kommt als Ereignis mira_agent_done

  @schritt6
  Szenario: AG-64 Aus Home Assistant einen Lauf abfragen
    Angenommen eine Aufruf-ID eines Laufs
    Wenn eine Automation die Aktion mira.agent_result mit dieser Aufruf-ID aufruft
    Dann bekommt sie den Lauf-Datensatz

  @schritt6
  Szenario: AG-65 Aus Home Assistant sind alle Agenten aufrufbar
    Angenommen ein beliebiger Agent, intern oder aus YAML
    Wenn eine Automation mira.agent mit seinem Namen und einer passenden Eingabe aufruft
    Dann läuft der Agent

  @schritt6
  Szenario: AG-66 Ein unbekannter Agent
    Wenn eine Automation mira.agent mit einem Namen aufruft, den es nicht gibt
    Dann scheitert der Aufruf mit diesem Grund

  # --- Modell (agenten.md: Modellwahl) ---------------------------------------------------------------

  @schritt1
  Szenario: AG-70 Ohne Eintrag für seine Rolle nimmt ein Agent main
    Angenommen in routing steht für die Rolle eines Agenten nichts
    Wenn der Agent läuft
    Dann nimmt er das Modell main

  @schritt1
  Szenario: AG-71 Mit Eintrag nimmt ein Agent das eingetragene Modell
    Angenommen in routing steht für die Rolle eines Agenten das Modell smart
    Wenn der Agent läuft
    Dann nimmt er das Modell smart

  @schritt4
  Szenario: AG-72 Den Delegate-Agenten gibt es auch ohne Eintrag
    Angenommen in routing steht für delegate nichts
    Wenn das Gespräch gerufen wird
    Dann hat es das Werkzeug delegate
    Und der Delegate-Agent nimmt das Modell main
