---
name: apotheke
description: "Apotheken mit Notdienst in der Nähe, heute („Welche Apotheke hat Notdienst?“, „Wo ist die nächste Notapotheke?“)."
license: MIT
---
# Apotheken-Notdienst

Die Notdienste liest du mit `web_page` von aponet.de, dem Portal der Apotheken, nach
Postleitzahl:

1. **Postleitzahl des Hauses:** Such sie im Gedächtnis (memory_search „Postleitzahl“).
   Sonst lies mit `web_page`
   `https://nominatim.openstreetmap.org/reverse?lat={{ home.latitude }}&lon={{ home.longitude }}&zoom=18&format=jsonv2`
   (JSON, `address.postcode`) und merk sie dir fürs Haus: memory_save mit house: true,
   „Die Postleitzahl des Hauses ist 76131.“ Nennt der Nutzer einen anderen Ort, nimm
   dessen Postleitzahl.
2. **Notdienste:** `https://www.aponet.de/apotheke/notdienstsuche/PLZ`. Du bekommst die
   Apotheken im Umkreis, die nächste zuerst, je mit Name, Entfernung, „Notdienst vom …
   bis …“, Adresse und Telefon.

So antwortest du: die nächste Apotheke mit Notdienst, Straße und Ort, Entfernung, bis
wann sie Dienst hat („Notdienst hat die Apotheke 29 in der Karlstraße 29, 600 Meter
entfernt, bis morgen früh halb neun.“). Die Telefonnummer, wenn der Nutzer fragt. Auf
Wunsch die nächsten zwei. Nenne nur, was die Seite liefert.

Steht in der Adresse oben statt Zahlen ein „-“, kennt Mira den Ort des Hauses nicht;
dann frag nach der Postleitzahl.
