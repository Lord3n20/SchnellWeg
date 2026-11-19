// Studentenverbindungen (D-50): Häuser aus dem App-Paket (assets/korporationen.json, gebaut mit
// scripts/korporationen.py aus der Couleurbummel-Liste, Quelle Wikipedia). Alles lokal: nichts geht an den Server,
// die Liste wird erst beim ersten Bedarf gelesen. Auf der Karte als kleine Fahne in den Farben, auf der Startseite
// die nächsten vier, in der Zielsuche als Treffer.
import 'dart:convert';
import 'dart:math';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'api.dart';
import 'bummel.dart';

const artKorporation = 'korporation';
const quelleKorporationen = 'Daten: Wikipedia (CC BY-SA 4.0) über die Couleurbummel-Liste, ungeprüft';

class Korporation {
  final String name, lang, adresse, verband, farben, gruendung, web, jourFixe, motto, farbPrinzip, fechtPrinzip;
  final double lat, lon;

  Korporation.json(Map<String, dynamic> j)
      : name = j['n'] as String,
        lang = j['l'] as String? ?? '',
        adresse = j['a'] as String? ?? '',
        verband = j['v'] as String? ?? '',
        farben = j['f'] as String? ?? '',
        gruendung = j['g'] as String? ?? '',
        web = j['w'] as String? ?? '',
        jourFixe = j['j'] as String? ?? '',
        motto = j['m'] as String? ?? '',
        farbPrinzip = j['fp'] as String? ?? '',
        fechtPrinzip = j['sp'] as String? ?? '',
        lat = (j['lat'] as num).toDouble(),
        lon = (j['lon'] as num).toDouble();

  /// Als Ziel; Detail ist die Anschrift (oder der Dachverband, wenn sie fehlt).
  Ort get ort => Ort(artKorporation, name, adresse.isNotEmpty ? adresse : verband, lat, lon);

  /// Gründungsjahr aus „1876“ oder „1844-11-14“.
  String get jahr => gruendung.length >= 4 ? gruendung.substring(0, 4) : gruendung;
}

/// Abstand in Metern (flache Näherung, reicht für einige zehn Kilometer).
double meterZwischen(double lat1, double lon1, double lat2, double lon2) {
  const r = 6371000.0;
  final x = (lon2 - lon1) * pi / 180 * cos((lat1 + lat2) / 2 * pi / 180);
  final y = (lat2 - lat1) * pi / 180;
  return sqrt(x * x + y * y) * r;
}

class Korporationen extends ChangeNotifier {
  static final instanz = Korporationen._();
  Korporationen._();

  List<Korporation> liste = const [];
  Future<void>? _laden;

  /// Liest die Liste einmal aus dem App-Paket; meldet sich danach über [notifyListeners].
  Future<void> laden() => _laden ??= rootBundle.loadString('assets/korporationen.json').then((s) {
        liste = lesen(s);
        notifyListeners();
      }, onError: (_) {});

  static List<Korporation> lesen(String s) => [
        for (final e in ((jsonDecode(s) as Map)['eintraege'] as List).cast<Map<String, dynamic>>()) Korporation.json(e)
      ];

  /// Die [n] nächsten Häuser bis [bisMeter], nach Abstand.
  List<(Korporation, double)> naechste(double lat, double lon, {int n = 4, double bisMeter = 25000}) {
    final l = [
      for (final k in liste)
        if (meterZwischen(lat, lon, k.lat, k.lon) case final m when m <= bisMeter) (k, m)
    ]..sort((a, b) => a.$2.compareTo(b.$2));
    return l.take(n).toList();
  }

  /// Treffer für die Zielsuche: alle Wörter der Eingabe in Name, Langname oder Anschrift.
  List<Korporation> suchen(String q, {int n = 6}) {
    final woerter = q.toLowerCase().split(RegExp(r'\s+')).where((w) => w.isNotEmpty).toList();
    if (woerter.isEmpty) return const [];
    return liste
        .where((k) {
          final t = '${k.name} ${k.lang} ${k.adresse} ${k.verband}'.toLowerCase();
          return woerter.every(t.contains);
        })
        .take(n)
        .toList();
  }
}

const _farbWerte = {
  'black': Color(0xFF111111),
  'white': Color(0xFFFFFFFF),
  'silver': Color(0xFFC0C0C0),
  'grey': Color(0xFF808080),
  'grey_light': Color(0xFFBDBDBD),
  'gold': Color(0xFFD4AF37),
  'yellow': Color(0xFFFFD600),
  'orange': Color(0xFFF57C00),
  'red': Color(0xFFD32F2F),
  'red_dark': Color(0xFF8E0000),
  'red_wine': Color(0xFF722F37),
  'red_bordeaux': Color(0xFF6D071A),
  'red_crimson': Color(0xFFDC143C),
  'pink': Color(0xFFF48FB1),
  'purple': Color(0xFF7B1FA2),
  'blue': Color(0xFF1565C0),
  'blue_dark': Color(0xFF0D2A6B),
  'blue_light': Color(0xFF64B5F6),
  'green': Color(0xFF2E7D32),
  'green_dark': Color(0xFF1B4D20),
  'green_light': Color(0xFF81C784),
  'green_moss': Color(0xFF6B7F3A),
  'brown': Color(0xFF6D4C41),
};

/// „blue-white-orange@white“ → Streifen von oben nach unten und Perkussion (Rand oben und unten); unbekannte
/// Farben fallen weg. Bei „black|white“ zählt die erste.
(List<Color>, Color?) farbenLesen(String s) {
  final teile = s.split('@');
  Color? farbe(String t) => _farbWerte[t.split('|').first.trim()];
  final streifen = [for (final t in teile.first.split('-')) ?farbe(t)];
  return (streifen, teile.length > 1 ? farbe(teile[1]) : null);
}

/// Kleine Fahne in den Farben der Verbindung (ohne bekannte Farben: Fahnen-Symbol).
class FarbBand extends StatelessWidget {
  final String farben;
  final double breite, hoehe;
  const FarbBand(this.farben, {super.key, this.breite = 24, this.hoehe = 16});

  @override
  Widget build(BuildContext context) {
    final (streifen, perkussion) = farbenLesen(farben);
    final rand = Theme.of(context).colorScheme.outline;
    if (streifen.isEmpty) return Icon(Icons.flag_outlined, size: hoehe + 4, color: rand);
    return Container(
      width: breite,
      height: hoehe,
      decoration: BoxDecoration(
        color: perkussion ?? streifen.first,
        border: Border.all(color: rand, width: 0.8),
        borderRadius: BorderRadius.circular(2),
      ),
      padding: perkussion == null ? null : EdgeInsets.symmetric(vertical: hoehe * 0.08),
      child: Column(children: [for (final c in streifen) Expanded(child: ColoredBox(color: c, child: const SizedBox.expand()))]),
    );
  }
}

/// Angaben zu einem Haus mit „Hierhin“; [hierhin] bekommt das Haus als Ziel.
Future<void> korporationZeigen(BuildContext context, Korporation k, {required void Function(Ort) hierhin, double? meter}) =>
    showModalBottomSheet<void>(
      context: context,
      showDragHandle: true,
      isScrollControlled: true,
      builder: (ctx) {
        final t = Theme.of(ctx).textTheme;
        final farbe = Theme.of(ctx).colorScheme;
        Widget zeile(IconData i, String s) => Padding(
              padding: const EdgeInsets.only(top: 6),
              child: Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Icon(i, size: 18, color: farbe.outline),
                const SizedBox(width: 10),
                Expanded(child: SelectableText(s)),
              ]),
            );
        final prinzip = [k.farbPrinzip, k.fechtPrinzip].where((s) => s.isNotEmpty && s != 'unbekannt').join(', ');
        return SafeArea(
          child: SingleChildScrollView(
            padding: const EdgeInsets.fromLTRB(20, 0, 20, 16),
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Row(children: [
                FarbBand(k.farben, breite: 36, hoehe: 24),
                const SizedBox(width: 12),
                Expanded(child: Text(k.name, style: t.titleMedium)),
              ]),
              if (k.lang.isNotEmpty) Padding(padding: const EdgeInsets.only(top: 4), child: Text(k.lang, style: t.bodySmall)),
              const SizedBox(height: 6),
              if (k.adresse.isNotEmpty) zeile(Icons.home_outlined, meter == null ? k.adresse : '${k.adresse} · ${_entfernung(meter)}'),
              if (k.verband.isNotEmpty) zeile(Icons.account_tree_outlined, k.verband),
              if (prinzip.isNotEmpty) zeile(Icons.info_outline, prinzip),
              if (k.jahr.isNotEmpty) zeile(Icons.cake_outlined, 'gegründet ${k.jahr}'),
              if (k.motto.isNotEmpty) zeile(Icons.format_quote, k.motto),
              if (k.jourFixe.isNotEmpty) zeile(Icons.event_repeat, k.jourFixe),
              if (k.web.isNotEmpty) zeile(Icons.link, k.web),
              BummelAbschnitt(k),
              const SizedBox(height: 12),
              Row(children: [
                Expanded(child: Text(quelleKorporationen, style: t.bodySmall?.copyWith(color: farbe.outline))),
                const SizedBox(width: 8),
                FilledButton.icon(
                  onPressed: () {
                    Navigator.pop(ctx);
                    hierhin(k.ort);
                  },
                  icon: const Icon(Icons.directions, size: 18),
                  label: const Text('Hierhin'),
                ),
              ]),
            ]),
          ),
        );
      },
    );

String _entfernung(double m) => m < 1000 ? '${(m / 10).round() * 10} m' : '${(m / 1000).toStringAsFixed(1).replaceAll('.', ',')} km';

/// Zeile der Startseite: Fahne, Name, Abstand.
class KorporationZeile extends StatelessWidget {
  final Korporation k;
  final double meter;
  final VoidCallback onTap;
  const KorporationZeile(this.k, this.meter, {super.key, required this.onTap});

  @override
  Widget build(BuildContext context) => InkWell(
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
          child: Row(children: [
            FarbBand(k.farben),
            const SizedBox(width: 12),
            Expanded(
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Text(k.name, maxLines: 1, overflow: TextOverflow.ellipsis, style: const TextStyle(fontWeight: FontWeight.w600)),
                if (k.adresse.isNotEmpty || k.verband.isNotEmpty)
                  Text([k.verband, k.adresse].where((s) => s.isNotEmpty).join(' · '),
                      maxLines: 1, overflow: TextOverflow.ellipsis, style: Theme.of(context).textTheme.bodySmall),
              ]),
            ),
            const SizedBox(width: 8),
            Text(_entfernung(meter), style: TextStyle(color: Theme.of(context).colorScheme.outline)),
          ]),
        ),
      );
}
