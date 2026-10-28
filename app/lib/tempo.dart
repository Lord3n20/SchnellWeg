// Tempo-Profile (D-12, D-21): zu Fuß, Einrad, Fahrrad – je von Hand gesetzt oder per Messgang (tempo_messen.dart).
// Das aktive Profil geht je Anfrage als `art` + `kmh` an /v1/route (Grenzen je Art wie dort).
// Bleibt nur auf dem Handy (tempo.json im App-Ordner).
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:path_provider/path_provider.dart';

const standardKmh = 4.3;

/// Art → (min, max) km/h; außerhalb antwortet /v1/route mit 400.
const grenzen = {'fuss': (2.0, 8.0), 'einrad': (4.0, 20.0), 'rad': (6.0, 35.0)};
const artName = {'fuss': 'Zu Fuß', 'einrad': 'Einrad', 'rad': 'Fahrrad'};

const _vorgaben = [
  ('entspannt', 'fuss', standardKmh),
  ('normal', 'fuss', 5.0),
  ('schnell', 'fuss', 6.0),
  ('Einrad', 'einrad', 12.0),
  ('Einrad schnell', 'einrad', 15.0),
  ('Fahrrad', 'rad', 16.0),
  ('Fahrrad schnell', 'rad', 22.0),
];

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
        final e = p[x.name] as Map<String, dynamic>?;
        final v = (e?['kmh'] as num?)?.toDouble();
        if (v == null || v < x.min || v > x.max) continue;
        x.kmh = v;
        x.gemessen = DateTime.tryParse(e!['gemessen'] as String? ?? '');
      }
      if (t.profile.any((x) => x.name == j['aktiv'])) t.aktiv = j['aktiv'] as String;
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
