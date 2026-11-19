# SchnellWeg aufs iPhone

Ohne App Store: Die App wird mit deiner eigenen (kostenlosen) Apple-ID signiert. Einmal richtest du dafür **SideStore** ein (dauert
etwa 20 Minuten und braucht einmal einen Computer), danach installierst und aktualisierst du SchnellWeg direkt am iPhone. Hintergrund: D-53.

## Was du brauchst
- iPhone mit iOS 15 oder neuer, eine Apple-ID (eine Zweit-ID geht auch).
- Für die Einrichtung einmal einen Computer (Windows, Mac oder Linux) und ein Kabel.
- Von Enzo: die Server-Adresse, eine Zertifikat-Datei (`.p12`) und das Passwort dazu.

## 1. SideStore einrichten (einmalig)
1. Der Anleitung auf **https://docs.sidestore.io** folgen („Installation“). Sie installiert SideStore mit deiner Apple-ID
   und richtet die kleine VPN-App ein, mit der SideStore die Apps später allein am Handy erneuert.
2. Ab iOS 16: **Einstellungen → Datenschutz & Sicherheit → Entwicklermodus** einschalten und das iPhone neu starten.
3. **Einstellungen → Allgemein → VPN & Geräteverwaltung**: deiner Apple-ID (Entwickler-App) vertrauen.

## 2. SchnellWeg installieren
1. SideStore öffnen → **Sources** → **+** → diese Adresse einfügen:
   `https://github.com/Lord3n20/SchnellWeg/releases/latest/download/quelle.json`
2. Bei SchnellWeg auf **Get/Free** tippen. Fertig, die App liegt auf dem Homescreen.

Ohne SideStore geht es auch mit **Sideloadly** am Computer: `SchnellWeg.ipa` aus dem neuesten Release
(https://github.com/Lord3n20/SchnellWeg/releases/latest) hineinziehen, Apple-ID eingeben, installieren. Dann musst du sie aber alle
7 Tage am Computer neu aufspielen.

## 3. Einrichten
1. Die `.p12`-Datei in der Dateien-App ablegen (z. B. per AirDrop oder Mail → „In Dateien sichern“).
2. SchnellWeg öffnen → Server-Adresse eintragen, **Client-Zertifikat wählen**, Passwort eingeben, speichern.
3. Die Frage nach dem Standort mit **Beim Verwenden der App erlauben** beantworten.

## Gut zu wissen
- **Alle 7 Tage erneuern:** Mit kostenloser Apple-ID läuft die Signatur nach 7 Tagen ab. SideStore erneuert im Hintergrund; sicherheitshalber
  ab und zu SideStore öffnen → **My Apps → Refresh All** (VPN-App an). Abgelaufen? Dann startet SchnellWeg nicht, bis du erneuerst –
  deine Favoriten und Einstellungen bleiben erhalten.
- **Updates:** SideStore zeigt neue Fassungen unter **My Apps** an; dort auf **Update** tippen. Nicht löschen und neu installieren,
  sonst sind Favoriten und Zugang weg.
- Mit einer kostenlosen Apple-ID gehen höchstens 3 solcher Apps gleichzeitig (SideStore zählt mit).
- Auf dem iPhone fehlen bisher: Messgang und Routen-Aufzeichnung, Ausstiegs-Erinnerung, Losgeh-Wecker und die Anzeige auf dem Sperrbildschirm.
  Standort gibt es nur, solange die App offen ist.
