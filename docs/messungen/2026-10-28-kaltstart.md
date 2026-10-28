# App-Kaltstart (28.10.2026)

Pixel 8 (GrapheneOS), Release-APK 10f6e73 (eigener Schlüssel, D-28), frisch installiert, per USB:
`am force-stop` → `am start -W` (TotalTime = bis zum ersten gezeichneten Bild der Activity).

| Lauf | 1 | 2 | 3 | 4 | 5 |
|---|---|---|---|---|---|
| TotalTime ms | 755 | 700 | 701 | 696 | (USB getrennt) |

Median ~700 ms (Ziel < 1 s, Grenze < 2 s) – erfüllt. Einschränkung: ohne eingerichteten Zugang (frische
Installation), also ohne Abfahrten-Abfrage; die Abfahrten kommen ohnehin erst nach dem ersten Bild
(lokale Halte-DB, b397db6). Mit Zugang, Karte und Bundesland gegenprüfen.
