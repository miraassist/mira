---
name: pakete
description: "Pakete – wann welches Paket kommt, aus den Versandmails der Paketdienste („Wann kommt mein Paket?“, „Kommt heute was?“)."
license: MIT
metadata:
  mira:
    mail:
      extract: >-
        Diese Mails kommen von Paketdiensten und Shops. Relevant ist eine Mail, die
        eine Sendung an den Empfänger ankündigt oder ihren Stand meldet (verschickt,
        unterwegs, kommt heute, zugestellt, liegt zur Abholung bereit, storniert,
        zurück an den Absender). Nicht relevant: Werbung, Rechnungen, Bewertungsbitten,
        Umfragen, Sendungen, die der Empfänger selbst verschickt.
        Titel: „Paket von <Shop oder Absender>“, mit dem Inhalt, wenn die Mail ihn
        nennt („Paket von Amazon: Druckerpatronen“); liegt es zur Abholung bereit:
        „Paket von <Shop> abholbereit“. Ist der Shop unbekannt, der Paketdienst
        („Paket per DHL“).
        Kennung: die Sendungsnummer; ohne sie die Bestellnummer des Shops; sonst leer.
        Status: angekündigt, verschickt, unterwegs, kommt heute, abholbereit:
        scheduled; zugestellt oder abgeholt: done; storniert oder zurück an den
        Absender: cancelled.
        Tag und Zeit: der angekündigte Zustelltag und, wenn genannt, das Zeitfenster
        (start und end). Nennt die Mail nur einen Zeitraum über mehrere Tage, den
        ersten Tag, ganztägig. Liegt das Paket zur Abholung bereit: der Tag der Mail,
        ganztägig, als Ort der PaketShop oder die Packstation mit Adresse; bis wann
        es dort liegt, gehört in die Einzelheiten. Zugestellt: der Tag der Mail.
        Einzelheiten: Paketdienst, Sendungsnummer, Bestellnummer, wo es abgelegt
        wurde. Nie Abholcodes, PINs, Passwörter oder Links.
---
# Pakete

Die Paketmails landen in Kalendern, die mit „Pakete“ anfangen: „Pakete“ gehört dem
ganzen Haus, „Pakete <Name>“ einer Person. Jeder Eintrag ist ein Paket: der Titel
nennt Shop und Inhalt, der Tag und das Zeitfenster sagen, wann es kommt, der Ort,
wo es abholbereit liegt. Status „done“ in der Beschreibung heißt: schon da.

Fragt jemand nach Paketen („Wann kommt mein Paket?“, „Kommt heute was?“, „Ist das
Paket von Amazon schon da?“), lies mit `calendar_events` den Kalender „Pakete“ und
den Pakete-Kalender der Person, die spricht, wenn es einen gibt, von heute an. Die
Pakete-Kalender anderer Personen nimmst du nicht.

- Nenne Tag, Zeitfenster („zwischen zehn und zwölf“), Shop und Inhalt.
- Abholbereit: wo es liegt und bis wann. Einen Abholcode nennst du nie, auch nicht
  auf Nachfrage; der steht in der Mail und in der App des Paketdienstes.
- Ist nichts eingetragen, sag das; Mira kennt nur Pakete, zu denen eine Mail kam.
- Ein Paket mit Status „done“ ist zugestellt; das sagst du, statt es anzukündigen.
