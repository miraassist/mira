---
name: postfach
description: "Miras Postfach: wie Mira Briefe und Mails auswertet, die sie lesen soll (in Paperless das Tag Mira, oder der Mail-Ordner Mira). Nur für die Auswertung; im Gespräch übernehmen die Skills termin und todo."
license: MIT
metadata:
  mira:
    inbox:
      extract: >-
        Es gibt zwei Arten, mit einem Dokument umzugehen. Ein Ding der Art
        appointment ist ein Termin: etwas, das zu einer Zeit stattfindet, oft an
        einem Ort (Elternabend, Feier, Ausflug, Arzttermin; Ferien und
        schulfreie Tage ganztägig, je einer pro Zeitraum). Der Skill termin
        trägt es auf das Ja in einen Kalender ein. Seine Felder: title (kurz,
        höchstens 80 Zeichen, „Elternabend“), proposals (die Vorschläge, einer
        pro Zeile: „2026-12-03 19:30–20:30 · Aula“ mit Beginn, Ende und Ort,
        oder „2026-12-22 ganztägig“, bei mehreren Tagen „2026-12-22 bis
        2027-01-06 ganztägig“; ohne genanntes Ende eine Stunde), description
        (was dazugehört, etwa „Unterlagen mitbringen“, und die Quelle). Mehrere
        Vorschläge nur, wenn das Dokument sie zur Auswahl anbietet (einer davon
        ist zu wählen); sonst ist jeder Termin ein eigenes Ding. Ein Ding der Art
        todo ist etwas, das die Person selbst tun muss: eine Frist („Rückmeldung
        bis Fr 28.11.“), eine Zahlung mit Betrag, ein Rückruf mit Name und
        Nummer. Der Skill todo setzt es auf das Ja auf ihre To-do-Liste. Seine
        Felder: item (kurz, höchstens 80 Zeichen, „Rechnung Stadtwerke
        bezahlen“), due (der Tag als YYYY-MM-DD, leer ohne Tag), description
        (was man dafür braucht, Betrag, Nummer, und die Quelle). Die Quelle ist
        etwa „aus Paperless 5444: Elternbrief der Grundschule“ oder „aus der
        Mail von Grundschule, 12.11.“. Die line nennt die Quelle kurz und das
        Ding mit Wochentag und Datum: „Elternbrief: Elternabend Do 3.12. 19:30,
        Aula“, „Steuerberater: Steuererklärung bis Mo 16.11.“. expires: bei einem
        Termin der Beginn des letzten Vorschlags, bei einem To-do der Tag der
        Fälligkeit, ohne ihn leer. Nimm nur Arten, die unter „Arten“ genannt
        sind. Kein Ding ist das Datum, an dem das Dokument geschrieben oder
        verschickt wurde, oder etwas, das schon vorbei ist und nichts mehr
        verlangt.
---
# Postfach

Dieser Skill sagt nur, wie Mira die Post auswertet. Was sie darin findet,
kommt als Termin (Skill termin) oder To-do (Skill todo) und wird dort
angesagt und eingetragen.
