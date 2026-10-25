---
name: fahrplan
description: "Abfahrten an der Haltestelle oder am Bahnhof („Wann fährt die nächste Bahn?“, „Wann geht der nächste Zug nach Stuttgart?“, „Hat die S-Bahn Verspätung?“)."
license: MIT
metadata:
  mira:
    settings:
      haltestelle:
        description: Die Haltestelle des Hauses, wie sie bei der Bahn oder im Verkehrsverbund heißt, z. B. "Karlsruhe Hbf" oder "Durlach Bahnhof".
        required: true
      verbund:
        description: Für Straßenbahn und Bus der Verkehrsverbund als Kürzel, z. B. KVV, VRR oder MVV. Leer heißt, nur Züge der Bahn.
---
# Fahrplan

Die Abfahrten liest du mit `web_page` von dbf.finalrewind.org, als JSON. Haltestelle:
„{{haltestelle}}“, Verbund: „{{verbund}}“. Nennt der Nutzer eine andere Haltestelle,
nimm die. In die Adresse kommt der Name mit %20 statt Leerzeichen, im Folgenden NAME.

- **Mit Verbund** (Straßenbahn, Bus und Züge im Verbund):
  `https://dbf.finalrewind.org/NAME.json?efa={{verbund}}&limit=30`.
  Je Abfahrt: `type` (Straßenbahn, Bus, S-Bahn …), `line`, `destination`, `countdown`
  (Minuten bis zur Abfahrt), `delay` (Minuten, leer ohne Echtzeit), `is_cancelled`,
  `platform_name`.
- **Ohne Verbund** (Züge der Bahn):
  `https://dbf.finalrewind.org/NAME.json?limit=30`.
  Je Abfahrt: `train` (z. B. „RE 2“, „S 3“), `destination`, `scheduledDeparture`
  (Uhrzeit; leer heißt, der Zug endet hier), `delayDeparture` (Minuten), `isCancelled`,
  `platform`, in `via` die Halte unterwegs und in `messages` Gründe für Verspätungen.

Kommt ein Fehler mit „name list“ zurück, ist der Name nicht eindeutig: Sag das und frag,
wie die Haltestelle genau heißt.

So antwortest du:
- **„Wann fährt die nächste …?“:** die nächste passende Abfahrt mit Linie, Ziel und
  Minuten oder Uhrzeit, eine Verspätung dazu („Die 6 Richtung Tivoli fährt in drei
  Minuten.“). Auf Wunsch die zwei, drei danach.
- **„… nach Stuttgart?“:** nur Abfahrten, deren Ziel oder `via` passt.
- **Ausfall oder Verspätung:** sag es zuerst, mit Grund, wenn einer dabei steht.
- Nenne nur, was die Abfrage liefert.
