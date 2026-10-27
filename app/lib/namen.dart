// Anzeigenamen aus den Rohnamen der Feeds. AVV-Halte tragen Richtung und Bahnsteig im Namen
// („Würselen Kaninsberg lw -> Haaren“, „Aachen Westbahnhof lw (Li.33) H2“): lw/sw = land-/stadtwärts,
// „-> X“ bzw. „(Li.33)“ = Richtung, H1…H15 bzw. ein Großbuchstabe = Bahnsteig. Die Abfahrten zeigen das Ziel
// ohnehin, also fällt das für die Anzeige weg; die Server-Daten bleiben roh.

final _richtungKlammer = RegExp(r'\s*\(\s*->[^)]*\)');
final _richtung = RegExp(r'\s*->.*$');
final _linie = RegExp(r'\s*\((Li\.\s*)?\d+[a-zA-Z]?\)');
final _lwsw = RegExp(r'\s+(lw|sw)\b');
final _bahnsteig = RegExp(r'\s+(H\d{1,2}|[A-Z])$');
final _leer = RegExp(r'\s{2,}');

String _grund(String s) => s
    .replaceAll(_richtungKlammer, '')
    .replaceAll(_richtung, '')
    .replaceAll(_linie, '')
    .replaceAll(_lwsw, '')
    .replaceAll(_leer, ' ')
    .trim();

/// Haltestellenname ohne Richtung und Bahnsteig.
String haltName(String roh) => _grund(roh).replaceAll(_bahnsteig, '').trim();

final _hBahnsteig = RegExp(r'\s+H\d{1,2}$');

/// Fahrtziel ohne Richtungskürzel, Liniennummer und Bahnsteig (nur „H4“, ein Großbuchstabe kann Teil des Ziels sein).
String zielName(String roh) => _grund(roh).replaceAll(_hBahnsteig, '');

/// Schlüssel zum Zusammenfassen: „Aachen Hbf“ (Bahn) und „Aachen Hauptbahnhof H1“ (Bus) sind derselbe Ort.
String haltSchluessel(String roh) => haltName(roh).toLowerCase().replaceAll(RegExp(r'\bhauptbahnhof\b'), 'hbf');
