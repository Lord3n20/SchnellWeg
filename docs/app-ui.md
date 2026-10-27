# App-Oberfläche (Vorgabe 27.10.2026)

## Startseite – ohne gewählte Route
```
┌──────────────────────────────┐
│ Von  [ Mein Standort    ◎ ]  │  Standort per Knopf direkt übernehmen
│ Nach [ Ziel …             ]  │  Vorschläge: Favoriten, zuletzt, Kalender-Termine mit Ort, Suche
├──────────────────────────────┤
│                              │
│            Karte             │  PMTiles offline, eigener Standort, Halte in der Nähe als Punkte
│                              │
├──────────────────────────────┤
│ Haltestellen in der Nähe     │
│ Elisenbrunnen   120 m        │
│   33 → Vaals   in 2 min (+1) │  je Halt die nächste Abfahrt (Echtzeit, sonst Fahrplan)
│ Bushof          350 m        │
│   45 → Uniklinik  in 4 min   │
└──────────────────────────────┘
```

## Startseite – Route ausgewählt
- Karte zeigt die Route (Fuß gestrichelt, Bus/Bahn farbig nach Linie, Umstiege als Punkte).
- Statt „Haltestellen in der Nähe“ eine **kurze Zusammenfassung**, z. B.
  `🚶 4 min → 33 Elisenbrunnen 12:41 (+1) → 🚶 2 min → an 13:02 · 1 Umstieg · Ampel grün`.
- Antippen der Zusammenfassung → Detailansicht (Phase später), Zurück → wieder Haltestellenliste.

## Datenschutz-Folgen (passt zu CLAUDE.md)
- Halte in der Nähe werden **auf dem Handy** aus der lokalen Haltestellen-SQLite bestimmt; der Server bekommt
  nur Halte-IDs für die nächsten Abfahrten, nie den Standort.
- Abfahrten-Abruf nur, solange die Startseite sichtbar ist (kein Hintergrund-Polling), Aktualisierung ≈ 30 s.
- Server braucht dafür einen Endpunkt „nächste Abfahrten für Halte-IDs“ (gebündelt, eine Anfrage für alle Halte).
