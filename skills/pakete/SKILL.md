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
        unterwegs, kommt heute, zugestellt, abgestellt, liegt zur Abholung bereit,
        storniert, zurück an den Absender). Nicht relevant: Werbung, Rechnungen,
        Bewertungsbitten, Umfragen, Sendungen, die der Empfänger selbst verschickt.
        Titel: „Paket von <Shop oder Absender>“, mit der Ware, wenn die Mail sie
        nennt („Paket von Amazon: Druckerpatronen“); nur die Ware, kurz, höchstens
        fünf Wörter, nie „1 Artikel“ oder eine Nummer. Liegt es zur Abholung bereit: „Paket von <Shop>
        abholbereit“. Ist der Shop unbekannt, der Paketdienst („Paket per DHL“).
        Kennungen (keys): alle Nummern, die diese Sendung oder Bestellung bezeichnen:
        Sendungsnummer, Bestellnummer, Sendungs-ID des Shops. Keine Kunden-,
        Rechnungs- oder Telefonnummern.
        Status: angekündigt, verschickt, unterwegs, kommt heute, abholbereit:
        scheduled; zugestellt, abgestellt (auch „wird am gewünschten Ort
        abgestellt“) oder abgeholt: done; storniert oder zurück an den Absender:
        cancelled.
        Tag und Zeit: der angekündigte Zustelltag und, wenn genannt, das Zeitfenster
        (start und end). Nennt die Mail eine Spanne („in 1–2 Werktagen“, „Mittwoch
        bis Freitag“), den ersten Tag, ganztägig. Nennt sie keinen Zustelltag (etwa
        nur „versandt“), bleibt day leer; der Versandtag ist kein Zustelltag.
        Abholbereit: der Tag der Mail, ganztägig. Zugestellt oder abgestellt: der
        Tag der Mail.
        Ort (location): nur wo das Paket abzuholen ist, PaketShop oder Packstation
        mit Adresse; sonst leer. Wo es abgestellt wird oder wurde, gehört in die
        Einzelheiten.
        Einzelheiten (details), je Zeile „Bezeichnung: Wert“, nur diese:
        „Paketdienst“, „Sendungsnummer“, „Bestellnummer“, „Ablageort“, „Abholung
        bis“. Nie Namen oder Adresse des Empfängers, Öffnungszeiten, Abholcodes,
        PINs, Passwörter oder Links.
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
