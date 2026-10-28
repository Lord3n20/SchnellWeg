// Tempo-Profile (D-12, D-21, D-22): zu Fuß, Einrad und vier Fahrradtypen – je von Hand gesetzt oder per Messgang
// (tempo_messen.dart).
// Das aktive Profil geht je Anfrage als `art` + `kmh` an /v1/route (Grenzen je Art wie dort).
// Bleibt nur auf dem Handy (tempo.json im App-Ordner).
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:path_provider/path_provider.dart';

const standardKmh = 4.3;

/// Art → (min, max) km/h; außerhalb antwortet /v1/route mit 400.
const grenzen = {
  'fuss': (2.0, 8.0),
  'einrad': (4.0, 20.0),
  'klapprad': (6.0, 30.0),
  'fixie': (6.0, 40.0),
  'rennrad': (10.0, 45.0),
  'mtb': (6.0, 35.0),
};
const artName = {
  'fuss': 'Zu Fuß',
  'einrad': 'Einrad',
  'klapprad': 'Klapprad',
  'fixie': 'Fixie',
  'rennrad': 'Rennrad',
  'mtb': 'Mountainbike',
};
/// Was die Art für die Route bedeutet (wie der Server sie rechnet, D-22).
const artInfo = {
  'fuss': 'Fußwege und Treppen',
  'einrad': 'Wege wie zu Fuß, fährt gratis in Bus und Bahn mit',
  'klapprad': 'Wege wie zu Fuß, zusammengeklappt in Bus und Bahn ohne Fahrradticket; Bus nur, wenn er 5 min spart',
  'fixie': 'Wege wie zu Fuß, Treppen getragen; nur Bahn, Fahrradticket nötig',
  'rennrad': 'Straßen und Radwege, keine Fußwege, Treppen nur zur Not; nur Bahn, Fahrradticket nötig',
  'mtb': 'Überall lang, meidet Treppen; nur Bahn, Fahrradticket nötig',
};
bool istRad(String art) => const {'klapprad', 'fixie', 'rennrad', 'mtb'}.contains(art);

const _vorgaben = [
  ('entspannt', 'fuss', standardKmh),
  ('normal', 'fuss', 5.0),
  ('schnell', 'fuss', 6.0),
  ('Einrad', 'einrad', 12.0),
  ('Einrad schnell', 'einrad', 15.0),
  ('Klapprad entspannt', 'klapprad', 12.0),
  ('Klapprad', 'klapprad', 15.0),
  ('Klapprad schnell', 'klapprad', 19.0),
  ('Fixie entspannt', 'fixie', 16.0),
  ('Fixie', 'fixie', 21.0),
  ('Fixie schnell', 'fixie', 26.0),
  ('Rennrad entspannt', 'rennrad', 18.0),
  ('Rennrad', 'rennrad', 25.0),
  ('Rennrad schnell', 'rennrad', 32.0),
  ('Mountainbike entspannt', 'mtb', 12.0),
  ('Mountainbike', 'mtb', 16.0),
  ('Mountainbike schnell', 'mtb', 20.0),
];
/// Profile vor D-22 (ein Fahrrad = heute Fixie): Werte und Auswahl werden übernommen.
const _alteNamen = {'Fixie': 'Fahrrad', 'Fixie schnell': 'Fahrrad schnell'};

class TempoProfil {
  final String name, art;
  double kmh;
  DateTime? gemessen; // null = Vorgabe oder von Hand

  TempoProfil(this.name, this.art, this.kmh, [this.gemessen]);

  double get min => grenzen[art]!.$1;
  double get max => grenzen[art]!.$2;

  Map<String, dynamic> toJson() => {'kmh': kmh, if (gemessen != null) 'gemessen': gemessen!.toIso8601String()};
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
      final p = j['profile'] as Map<String, dynamic>;
      for (final x in t.profile) {
        final e = (p[x.name] ?? p[_alteNamen[x.name]]) as Map<String, dynamic>?;
        final v = (e?['kmh'] as num?)?.toDouble();
        if (v == null || v < x.min || v > x.max) continue;
        x.kmh = v;
        x.gemessen = DateTime.tryParse(e!['gemessen'] as String? ?? '');
      }
      final aktiv = _alteNamen.entries.where((e) => e.value == j['aktiv']).firstOrNull?.key ?? j['aktiv'];
      if (t.profile.any((x) => x.name == aktiv)) t.aktiv = aktiv as String;
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

  /// Nur das aktive Profil (für die Startseite).
  static Future<TempoProfil> laden() async => (await oeffnen()).aktuell;

  Future<void> speichern() async => (await _datei('tempo.json')).writeAsString(jsonEncode({
        'aktiv': aktiv,
        'profile': {for (final p in profile) p.name: p.toJson()},
      }));
}

String tempoText(double kmh) => '${kmh.toStringAsFixed(1).replaceAll('.', ',')} km/h';

/// Regler für ein Profil als Bottom Sheet; liefert das neue Tempo oder null, wenn nichts geändert wurde.
Future<double?> tempoWaehlen(BuildContext context, TempoProfil p) => showModalBottomSheet<double>(
      context: context,
      builder: (_) => _TempoBlatt(p),
    );

class _TempoBlatt extends StatefulWidget {
  final TempoProfil p;
  const _TempoBlatt(this.p);

  @override
  State<_TempoBlatt> createState() => _TempoBlattState();
}

class _TempoBlattState extends State<_TempoBlatt> {
  late double _kmh = widget.p.kmh;

  @override
  Widget build(BuildContext context) {
    final p = widget.p;
    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 8),
        child: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.start, children: [
          Text('${p.name}: ${tempoText(_kmh)}', style: Theme.of(context).textTheme.titleMedium),
          Slider(
            value: _kmh,
            min: p.min,
            max: p.max,
            divisions: ((p.max - p.min) * 10).round(),
            label: tempoText(_kmh),
            onChanged: (v) => setState(() => _kmh = v),
          ),
          Align(
            alignment: Alignment.centerRight,
            child: FilledButton(
              onPressed: () => Navigator.of(context).pop((_kmh - p.kmh).abs() < 0.05 ? null : _kmh),
              child: const Text('Übernehmen'),
            ),
          ),
        ]),
      ),
    );
  }
}
