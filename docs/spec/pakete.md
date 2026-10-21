# Pakete – Spezifikation

Stand 2026-10-20. Beschlossen, gebaut (0.6.57). Die erste Anwendung des [Mail-Kalenders](mail-kalender.md); wie Mails
hereinkommen, ausgewertet und eingetragen werden, steht dort.

## Ziel

„Wann kommt mein Paket?“ – „Kommt heute was?“ – im Briefing: „Heute kommt zwischen 10 und 12 Uhr dein Paket von
Amazon.“

Die Schnittstellen der Paketdienste helfen wenig: Sie brauchen die Sendungsnummer, die man vorher nicht kennt, und
liefern Privatkunden oft kein Zeitfenster. Die Versandmails haben beides und mehr, und das für alle Versender gleich.

## Einrichten

- In HA einen **IMAP-Eintrag** anlegen, mit einer Suche nach den Absendern der Paketdienste, etwa
  `OR OR FROM dhl.de FROM hermesworld.com FROM gls-group.eu` (Ordner: Posteingang).
- Das **Gerät** des Eintrags **„Pakete“** nennen (fürs Haus) oder **„Pakete <Login>“** (für eine Person, etwa
  „Pakete stefan“). Der Name des Eintrags selbst zählt nicht.
- Den Kalender gleichen Namens legt Mira an, wenn es ihn nicht gibt.

## Skill `pakete`

**Auswertung** (`mail.extract`): Was die Anleitung dem isolierten Aufruf sagt:

- Relevant ist eine Mail, die eine Sendung an den Nutzer ankündigt oder ihren Stand meldet; Werbung, Rechnungen und
  Bewertungsbitten nicht.
- **Titel:** „Paket von <Shop>“, mit der Ware, wenn die Mail sie nennt („Paket von Amazon: Druckerpatronen“), kurz (höchstens fünf Wörter), nie
  „1 Artikel“ oder eine Nummer; liegt es zur Abholung bereit: „Paket von <Shop> abholbereit“.
- **Kennungen:** alle Nummern der Sendung oder Bestellung: Sendungsnummer, Bestellnummer, Sendungs-ID des Shops. So
  findet die Mail des Paketdienstes den Eintrag aus der Mail des Shops.
- **Status:** angekündigt, verschickt, unterwegs, heute, abholbereit: `scheduled`; zugestellt, abgestellt oder
  abgeholt: `done`; storniert oder zurückgeschickt: `cancelled`.
- **Tag und Zeit:** der angekündigte Zustelltag und das Zeitfenster; bei einer Spanne („in 1–2 Werktagen“) der erste
  Tag. **Ohne Zustelltag** (nur „versandt“) kein Eintrag; der Versandtag ist kein Zustelltag. Abholbereit,
  zugestellt: der Tag der Mail.
- **Ort:** nur der Abholort (PaketShop, Packstation); wo es abgestellt wird, steht in den Einzelheiten.
- **Einzelheiten:** nur Paketdienst, Sendungsnummer, Bestellnummer, Ablageort, Abholung bis. **Nie Namen oder Adresse
  des Empfängers, Abholcodes, PINs oder Links.**

**Zugestellt** (`mail.done: "●"`): Ein zugestelltes oder abgeholtes Paket steht als „● Paket von …“ im Kalender,
bis zum Ende des Tages.

**Fragen** (Text des Skills): „Wann kommt mein Paket?“ liest den Kalender „Pakete“ und den eigenen der Person
(„Pakete <Login>“, wenn die Person bekannt ist). Mira nennt Tag, Zeitfenster, Shop und Inhalt; Abholort ja, Codes nie.

## Briefing

Das persönliche Briefing liest „Pakete {{person_login}}“ mit: „Heute kommt zwischen 10 und 12 dein Paket von
Amazon.“ Das Haus-Briefing liest „Pakete“, wenn er in seiner Einstellung `kalender` steht.

## Grenzen

- Microsoft 365 und Outlook.com gehen mit der IMAP-Integration von HA nicht (kein OAuth); Gmail braucht ein
  App-Passwort.
- Zeigt ein Versender das Zeitfenster nur auf seiner Webseite und nicht in der Mail, kennt Mira nur den Tag.
