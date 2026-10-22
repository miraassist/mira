---
name: ferien
description: "Schulferien und Feiertage im eigenen Bundesland („Wann sind die nächsten Ferien?“, „Ist Montag ein Feiertag?“, „Wie lange gehen die Herbstferien?“)."
license: MIT
metadata:
  mira:
    settings:
      bundesland:
        description: Dein Bundesland als Kürzel, z. B. BW, BY, NW oder HE.
        required: true
---
# Ferien und Feiertage

Du liest beides mit `web_page`, als JSON. Bundesland: {{bundesland}}.

- **Schulferien:**
  `https://openholidaysapi.org/SchoolHolidays?countryIsoCode=DE&subdivisionCode=DE-{{bundesland}}&validFrom=VON&validTo=BIS&languageIsoCode=DE`
  mit VON und BIS als JJJJ-MM-TT, meist heute bis in einem Jahr. Je Eintrag
  `startDate`, `endDate` (der letzte Ferientag) und `name` (`text`). Einzelne
  Feiertage stehen auch dazwischen, etwa „Reformationsfest“; Ferien sind die
  Einträge, die mehrere Tage dauern oder „ferien“ im Namen haben.
- **Feiertage:** `https://feiertage-api.de/api/?jahr=JJJJ&nur_land={{bundesland}}`.
  Du bekommst die gesetzlichen Feiertage des Jahres mit Namen und `datum`.

So antwortest du:
- **„Wann sind die nächsten Ferien?“:** Name, erster und letzter Tag mit Wochentag
  („Die Herbstferien gehen von Montag, dem 26., bis Freitag, dem 30. Oktober.“).
  Laufen gerade Ferien, sag, bis wann.
- **„Ist Montag ein Feiertag?“:** ja mit Namen, oder nein. Rechne den Tag mit dem
  heutigen Datum aus.
- Nenne nur Tage, die die Abfrage liefert. Brückentage rechnest du nur aus, wenn
  danach gefragt wird.
