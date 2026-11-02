// Profile (D-12, D-33, D-34): „Zu Fuß“, „Auto“ und beliebig viele eigene – je Name, Tempo (von Hand oder per Messgang,
// tempo_messen.dart) und was die Route meiden soll. Das alles bleibt auf dem Handy (tempo.json im App-Ordner); an
// /v1/route gehen je Anfrage nur `kmh` und `meiden`, beim Auto dazu `art`.
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:path_provider/path_provider.dart';

const standardKmh = 4.3;

/// Art → (min, max) km/h; außerhalb antwortet /v1/route mit 400. Eigene Profile: alles über 0 (min ausschließlich).
const grenzen = {
  'fuss': (2.0, 8.0),
  'eigen': (0.0, double.infinity),
  'auto': (30.0, 200.0),
};
const artName = {'fuss': 'Zu Fuß', 'eigen': 'Eigenes Profil', 'auto': 'Auto'};

/// Neue Installation: nur diese; der Rest sind eigene Profile (D-34).
const _vorgaben = [
  ('entspannt', 'fuss', standardKmh), // ein Gehtempo (D-33); Schlüssel bleibt, die Schnelleinstellung nutzt ihn
  ('Auto', 'auto', 130.0),
];
/// Feste Profile vor D-34: werden beim ersten Öffnen einmalig zu eigenen (Name, Tempo, was sie meiden wie vorher, Ticket).
const _fahrradTreppen = {'fusswege': true, 'treppen': 0.5, 'belag': 0.5, 'bus': true};
const _rennrad = {'fusswege': true, 'treppen': 1.0, 'belag': 0.8, 'bus': true};
const _mtb = {'fusswege': true, 'treppen': 0.3, 'bus': true};
const _klapprad = {'fusswege': true, 'treppen': 0.2, 'belag': 0.5, 'bus_min': 5};
const _altProfile = <(String, double, Map<String, Object>, bool)>[
  ('Einrad', 12.0, {}, false),
  ('Einrad schnell', 15.0, {}, false),
  ('Klapprad entspannt', 12.0, _klapprad, false),
  ('Klapprad', 15.0, _klapprad, false),
  ('Klapprad schnell', 19.0, _klapprad, false),
  ('Fixie entspannt', 16.0, _fahrradTreppen, true),
  ('Fixie', 21.0, _fahrradTreppen, true),
  ('Fixie schnell', 26.0, _fahrradTreppen, true),
  ('Rennrad entspannt', 18.0, _rennrad, true),
  ('Rennrad', 25.0, _rennrad, true),
  ('Rennrad schnell', 32.0, _rennrad, true),
  ('Mountainbike entspannt', 12.0, _mtb, true),
  ('Mountainbike', 16.0, _mtb, true),
  ('Mountainbike schnell', 20.0, _mtb, true),
];
/// Profile vor D-22 (ein Fahrrad = heute Fixie): Werte und Auswahl werden übernommen.
const _alteNamen = {'Fixie': 'Fahrrad', 'Fixie schnell': 'Fahrrad schnell'};
/// Anzeigenamen, solange keiner selbst vergeben ist (die Kachel nutzt dieselben).
const _standardTitel = {'entspannt': 'Zu Fuß', 'Einrad': 'Custom 1', 'Einrad schnell': 'Custom 2'};
/// Frühere Fuß-Profile (vor D-33): war eines aktiv, gilt sein Tempo als das eine Gehtempo.
const _alteFuss = {'normal', 'schnell'};

/// Puffer ums Gehtempo in beide Richtungen (D-33): so weit daneben gilt noch als normales Tempo.
const tempoPuffer = 0.2;

enum GehStufe { entspannter, normal, schneller }

/// Wie man zum ersten Halt gehen muss: Zugangsweg `weg` (beim Gehtempo `kmh` gerechnet), `frei` bis zur Abfahrt.
/// Liefert Stufe und nötiges Tempo; null, wenn die Abfahrt schon vorbei ist.
(GehStufe, double)? gehHinweis(Duration weg, Duration frei, double kmh) {
  if (frei <= Duration.zero) return null;
  final noetig = kmh * weg.inSeconds / frei.inSeconds;
  if (noetig < kmh * (1 - tempoPuffer)) return (GehStufe.entspannter, noetig);
  if (noetig > kmh * (1 + tempoPuffer)) return (GehStufe.schneller, noetig);
  return (GehStufe.normal, noetig);
}

class TempoProfil {
  final String name, art; // name: fester Schlüssel in tempo.json; art: fuss | eigen | auto
  double kmh;
  DateTime? gemessen; // null = Vorgabe oder von Hand
  String? titel; // selbst vergebener Name
  Map<String, dynamic> meiden; // Stufen 0–1 und Schalter wie in /v1/route (D-34); fehlt = egal
  bool ticket; // Fahrradticket nötig: nur Anzeige in der App
  bool fern; // Fernverkehr zulassen

  TempoProfil(this.name, this.art, this.kmh, [this.gemessen]) : meiden = {}, ticket = false, fern = false;

  TempoProfil.json(this.name, Map<String, dynamic> j)
      : art = j['art'] as String,
        kmh = (j['kmh'] as num).toDouble(),
        gemessen = DateTime.tryParse(j['gemessen'] as String? ?? ''),
        titel = j['titel'] as String?,
        meiden = Map.of(j['meiden'] as Map<String, dynamic>? ?? const {}),
        ticket = j['ticket'] == true,
        fern = j['fern'] == true;

  TempoProfil kopie() => TempoProfil.json(name, jsonDecode(jsonEncode(toJson())) as Map<String, dynamic>);

  /// Name für die Anzeige.
  String get anzeige => titel ?? _standardTitel[name] ?? name;
  bool get eigen => art == 'eigen';
  bool get frei => max.isInfinite; // jedes Tempo über 0
  bool get messbar => art != 'auto'; // das Auto hat ein Höchsttempo, kein Reisetempo
  bool erlaubt(double v) => v > 0 && v >= min && v <= max;
  IconData get icon => switch (art) {
        'fuss' => Icons.directions_walk,
        'auto' => Icons.directions_car,
        _ => meiden['fusswege'] == true ? Icons.directions_bike : Icons.circle_outlined,
      };

  /// Nur für die Anzeige der Wege ohne Fahrt.
  String get wegArt => art == 'fuss' ? 'fuss' : (meiden['fusswege'] == true ? 'rad' : 'eigen');

  double get min => grenzen[art]!.$1;
  double get max => grenzen[art]!.$2;

  /// Für /v1/route: `art` nur bei festen Regeln (Auto; zu Fuß ohne Meiden wie ältere Apps), sonst entscheidet `meiden`.
  String? get serverArt => art == 'auto' || (art == 'fuss' && meiden.isEmpty) ? art : null;
  Map<String, dynamic>? get serverMeiden => meiden.isEmpty && !eigen ? null : meiden;

  double stufe(String k) => (meiden[k] as num?)?.toDouble() ?? 0;

  /// Wert setzen; 0, false und null fallen weg, damit „nichts meiden“ leer bleibt.
  void setzen(String k, Object? v) => v == null || v == 0 || v == 0.0 || v == false ? meiden.remove(k) : meiden[k] = v;

  bool gleich(TempoProfil o) => name == o.name && jsonEncode(toJson()) == jsonEncode(o.toJson());

  Map<String, dynamic> toJson() => {
        'art': art,
        'kmh': kmh,
        if (gemessen != null) 'gemessen': gemessen!.toIso8601String(),
        if (titel != null) 'titel': titel,
        if (meiden.isNotEmpty) 'meiden': meiden,
        if (ticket) 'ticket': true,
        if (fern) 'fern': true,
      };
}

class Tempo {
  List<TempoProfil> profile;
  String aktiv;

  Tempo._(this.profile, this.aktiv);

  TempoProfil get aktuell => profile.firstWhere((p) => p.name == aktiv);

  static Future<File> _datei(String name) async => File('${(await getApplicationSupportDirectory()).path}/$name');

  static Tempo _vorgabe() => Tempo._([for (final (n, a, v) in _vorgaben) TempoProfil(n, a, v)], 'entspannt');

  static Future<Tempo> oeffnen() async {
    final t = _vorgabe();
    try {
      final j = jsonDecode(await (await _datei('tempo.json')).readAsString()) as Map<String, dynamic>;
      t._uebernehmen(j);
      if (j['v'] != 2) await t.speichern(); // nur einmal umstellen; die Kachel liest die Profile aus der Datei
      return t;
    } on FileSystemException {
      // noch nie gespeichert
    } on FormatException {
      return t;
    }
    // Einzelwert aus der Zeit vor den Profilen übernehmen: ins nächstliegende Fuß-Profil, das dann aktiv ist.
    final alt = await _datei('tempo');
    try {
      final v = double.tryParse(await alt.readAsString());
      final (lo, hi) = grenzen['fuss']!;
      if (v != null && v >= lo && v <= hi) {
        final p = t.profile.where((p) => p.art == 'fuss').reduce((a, b) => (a.kmh - v).abs() <= (b.kmh - v).abs() ? a : b);
        p.kmh = v;
        t.aktiv = p.name;
        await t.speichern();
      }
      await alt.delete();
    } on FileSystemException {
      // nichts da
    }
    return t;
  }

  /// Gespeicherten Stand in die Vorgaben übernehmen; ältere Stände werden dabei umgestellt.
  void _uebernehmen(Map<String, dynamic> j) {
    final p = j['profile'] as Map<String, dynamic>;
    if (j['v'] == 2) {
      for (final MapEntry(:key, :value) in p.entries) {
        final x = TempoProfil.json(key, value as Map<String, dynamic>);
        final i = profile.indexWhere((a) => a.name == key);
        i >= 0 ? profile[i] = x : profile.add(x);
      }
    } else {
      // Stand vor D-34: die festen Profile werden zu eigenen, das Auto bleibt hinter „Zu Fuß“
      for (final (n, v, m, ticket) in _altProfile) {
        profile.add(TempoProfil(n, 'eigen', v)
          ..meiden = Map.of(m)
          ..ticket = ticket
          ..titel = _standardTitel[n]);
      }
      final alterFuss = _alteFuss.contains(j['aktiv']) ? j['aktiv'] : null;
      for (final x in profile) {
        final e = ((x.name == 'entspannt' ? p[alterFuss] : null) ?? p[x.name] ?? p[_alteNamen[x.name]]) as Map<String, dynamic>?;
        if (x.eigen) x.titel = e?['titel'] as String? ?? x.titel;
        final v = (e?['kmh'] as num?)?.toDouble();
        if (v == null || !x.erlaubt(v)) continue;
        x.kmh = v;
        x.gemessen = DateTime.tryParse(e!['gemessen'] as String? ?? '');
      }
    }
    final a = _alteNamen.entries.where((e) => e.value == j['aktiv']).firstOrNull?.key ?? j['aktiv'];
    if (profile.any((x) => x.name == a)) aktiv = a as String;
  }

  /// Nur zum Testen der Umstellung älterer Stände.
  @visibleForTesting
  static Tempo ausJson(Map<String, dynamic> j) => _vorgabe().._uebernehmen(j);

  /// Nur das aktive Profil (für die Startseite).
  static Future<TempoProfil> laden() async => (await oeffnen()).aktuell;

  Future<void> speichern() async => (await _datei('tempo.json')).writeAsString(jsonEncode({
        'v': 2,
        'aktiv': aktiv,
        'profile': {for (final p in profile) p.name: p.toJson()},
      }));
}

String tempoText(double kmh) => '${kmh.toStringAsFixed(1).replaceAll('.', ',')} km/h';

/// Kurzauswahl Verkehrsmittel (Startseite): alle Profile in einer Liste, Antippen macht es aktiv und speichert.
/// Liefert das neue aktive Profil oder null, wenn nichts geändert wurde.
Future<TempoProfil?> verkehrsmittelWaehlen(BuildContext context) async {
  final t = await Tempo.oeffnen();
  if (!context.mounted) return null;
  final p = await showModalBottomSheet<TempoProfil>(
    context: context,
    isScrollControlled: true,
    builder: (ctx) => DraggableScrollableSheet(
      expand: false,
      initialChildSize: 0.6,
      maxChildSize: 0.9,
      builder: (ctx, scroll) => ListView(controller: scroll, children: [
        for (final p in t.profile)
          ListTile(
            dense: true,
            leading: Icon(p.icon),
            title: Text(p.anzeige),
            trailing: Text(tempoText(p.kmh)),
            selected: p.name == t.aktiv,
            onTap: () => Navigator.pop(ctx, p),
          ),
      ]),
    ),
  );
  if (p == null || p.name == t.aktiv) return null;
  t.aktiv = p.name;
  await t.speichern();
  return p;
}
