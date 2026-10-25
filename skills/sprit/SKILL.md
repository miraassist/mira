---
name: sprit
description: "Spritpreise an den Tankstellen in der Nähe („Wo ist Diesel gerade günstig?“, „Was kostet Super E10?“, „Lohnt sich Tanken jetzt?“)."
license: MIT
---
# Spritpreise

Die Preise kommen aus der Home-Assistant-Integration **Tankerkönig**: Sie legt je
Tankstelle und Sorte einen Sensor an (Preis in €/L, im Namen Tankstelle und Sorte, etwa
„Aral Durlacher Allee Diesel“). Lies sie mit GetLiveContext.

- **„Wo ist … günstig?“:** die Sorte des Nutzers (Diesel, Super E5, Super E10), die
  zwei, drei günstigsten Tankstellen mit Preis, die günstigste zuerst („Diesel ist am
  günstigsten bei Aral in der Durlacher Allee, 1,62 €.“). Preise mit zwei Stellen nach
  dem Komma sagen.
- Fragt der Nutzer ohne Sorte, nimm die aus dem Gedächtnis („Ben tankt Diesel“); sonst
  frag und biete an, sie dir zu merken.
- **„Lohnt sich Tanken jetzt?“:** Du kennst nur den Preis jetzt, keinen Verlauf. Sag den
  günstigsten Preis und, als Faustregel, dass es abends meist günstiger ist als morgens.
- Ist eine Tankstelle geschlossen, steht ihr Sensor auf unbekannt oder nicht verfügbar:
  lass sie weg.
- Findest du keine Tankerkönig-Sensoren, sag, dass dafür in Home Assistant die
  Integration Tankerkönig eingerichtet und ihre Sensoren für Assist freigegeben sein
  müssen.
