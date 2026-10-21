# Parcels – spec

Status 2026-10-20: decided, built (0.6.57). The first application of the [mail calendar](mail-kalender.md); how mails
come in, are read and entered is described there.

## Target

"Wann kommt mein Paket?" – "Kommt heute was?" – in the briefing: "Heute kommt zwischen 10 und 12 Uhr dein Paket von
Amazon."

The carriers' interfaces help little: They need the tracking number, which one does not know beforehand, and
often deliver no time window to private customers. The shipping mails have both and more, and the same for all senders.

## Setup

- In HA create an **IMAP entry**, with a search for the senders of the carriers, e.g.
  `OR OR FROM dhl.de FROM hermesworld.com FROM gls-group.eu` (folder: inbox).
- Name the **device** of the entry **"Pakete"** (for the house) or **"Pakete <login>"** (for a person, e.g.
  "Pakete stefan"). The name of the entry itself does not count.
- Mira creates the calendar of the same name if it does not exist.

## Skill `pakete`

**Reading** (`mail.extract`): What the instructions tell the isolated call:

- Relevant is a mail that announces a shipment to the user or reports its status; advertising, invoices and
  review requests are not.
- **Title:** "Paket von <Shop>", with the goods if the mail names them ("Paket von Amazon: Druckerpatronen"), short (at most five words), never
  "1 Artikel" or a number; if it is ready for pickup: "Paket von <Shop> abholbereit".
- **Keys:** all numbers of the shipment or order: tracking number, order number, shipment ID of the shop. So
  the carrier's mail finds the entry from the shop's mail.
- **Status:** announced, shipped, on the way, today, ready for pickup: `scheduled`; delivered, dropped off or
  picked up: `done`; cancelled or returned: `cancelled`.
- **Day and time:** the announced delivery day and the time window; with a range ("in 1–2 Werktagen") the first
  day. **Without a delivery day** (only "shipped") no entry; the shipping day is not a delivery day. Ready for pickup,
  delivered: the day of the mail.
- **Location:** only the pickup place (PaketShop, Packstation); where it is dropped off is in the details.
- **Details:** only carrier, tracking number, order number, drop-off place, pickup until. **Never names or address
  of the recipient, pickup codes, PINs or links.**

**Delivered** (`mail.done: "●"`): A delivered or picked-up parcel stands as "● Paket von …" in the calendar,
until the end of the day.

**Questions** (text of the skill): "Wann kommt mein Paket?" reads the calendar "Pakete" and the person's own
("Pakete <login>", if the person is known). Mira names day, time window, shop and contents; pickup place yes, codes never.

## Briefing

The personal briefing reads "Pakete {{person_login}}" too: "Heute kommt zwischen 10 und 12 dein Paket von
Amazon." The house briefing reads "Pakete" if it is in its setting `kalender`.

## Limits

- Microsoft 365 and Outlook.com do not work with HA's IMAP integration (no OAuth); Gmail needs an
  app password.
- If a sender shows the time window only on its website and not in the mail, Mira knows only the day.
