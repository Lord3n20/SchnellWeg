// Abfahrten einer einzelnen Haltestelle (alle Bahnsteige gleichen Namens), geöffnet durch Antippen auf der Karte oder
// in der Liste; erscheinen auf der Startseite unter der Karte, abgefragt im Takt der Startseite.
import 'package:flutter/material.dart';

import 'api.dart';
import 'namen.dart';

const _maxIds = 30; // Grenze von /v1/departures
const maxHaltZeilen = 20;

/// Die Haltestelle als Ort (Mitte ihrer Bahnsteige), etwa als Ziel.
Ort halteOrt(Halt h, Iterable<Halt> bahnsteige) {
  final l = bahnsteige.toList();
  return Ort('halt', haltName(h.name), 'Haltestelle', l.map((x) => x.lat).reduce((a, b) => a + b) / l.length,
      l.map((x) => x.lon).reduce((a, b) => a + b) / l.length);
}

/// Bahnsteige derselben Haltestelle wie [h]; [h] ist immer dabei, höchstens so viele, wie der Server annimmt.
List<Halt> bahnsteigeVon(Halt h, Iterable<Halt> umgebung) {
  final k = haltSchluessel(h.name);
  return [h, ...umgebung.where((x) => x.id != h.id && haltSchluessel(x.name) == k)].take(_maxIds).toList();
}

String wannText(DateTime t, DateTime jetzt) {
  final min = t.difference(jetzt).inSeconds / 60;
  if (min < 0.5) return 'jetzt';
  if (min < 60) return 'in ${min.round()} min';
  return '${t.hour.toString().padLeft(2, '0')}:${t.minute.toString().padLeft(2, '0')}';
}

/// „33 → Vaals   in 2 min (+1)“
class AbfahrtZeile extends StatelessWidget {
  final Abfahrt a;
  final DateTime jetzt;
  const AbfahrtZeile(this.a, this.jetzt, {super.key});

  @override
  Widget build(BuildContext context) {
    final farbe = Theme.of(context).colorScheme;
    return Text.rich(TextSpan(children: [
      TextSpan(text: a.line, style: const TextStyle(fontWeight: FontWeight.w600)),
      TextSpan(text: ' → ${zielName(a.headsign)}   '),
      if (a.canceled)
        TextSpan(text: 'fällt aus', style: TextStyle(color: farbe.error))
      else ...[
        TextSpan(text: wannText(a.erwartet, jetzt)),
        if ((a.versp ?? 0) != 0)
          TextSpan(
            text: ' (${a.versp! > 0 ? '+' : ''}${a.versp})',
            style: TextStyle(color: a.versp! > 0 ? farbe.error : farbe.primary),
          ),
      ],
    ]));
  }
}

/// Unterer Bereich der Startseite, solange eine Haltestelle gewählt ist; [abfahrten] null = lädt noch.
class HaltAbfahrten extends StatelessWidget {
  final List<Abfahrt>? abfahrten;
  final String? fehler;
  final Future<void> Function() onRefresh;
  const HaltAbfahrten({super.key, required this.abfahrten, required this.fehler, required this.onRefresh});

  @override
  Widget build(BuildContext context) {
    final l = abfahrten;
    if (l == null && fehler == null) return const Center(child: CircularProgressIndicator());
    final jetzt = DateTime.now();
    return RefreshIndicator(
      onRefresh: onRefresh,
      child: ListView(padding: const EdgeInsets.fromLTRB(16, 0, 16, 8), children: [
        if (fehler != null)
          Padding(
            padding: const EdgeInsets.only(bottom: 8),
            child: Text(fehler!, style: TextStyle(color: Theme.of(context).colorScheme.error)),
          ),
        if (l != null && l.isEmpty) const Text('Keine Abfahrten in den nächsten 2 Stunden'),
        for (final a in (l ?? const <Abfahrt>[]).take(maxHaltZeilen))
          Padding(padding: const EdgeInsets.symmetric(vertical: 4), child: AbfahrtZeile(a, jetzt)),
      ]),
    );
  }
}
