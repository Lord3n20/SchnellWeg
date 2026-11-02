// Verkehrslage bei Autorouten (D-38): Staus, Sperrungen und Baustellen auf der Route, Quelle Autobahn-API über den Server.
// Nur Autobahnen, die Daten kennen weder Land- noch Bundesstraßen.
import 'package:flutter/material.dart';

import 'api.dart';

String meldungText(Meldung m) => switch (m.art) {
      'sperrung' => '⛔ Sperrung ${m.titel}',
      'baustelle' => '🚧 Baustelle ${m.titel}${m.maxKmh > 0 ? ' · max. ${m.maxKmh} km/h' : ''}',
      _ => '🔴 ${m.gesperrt ? 'Gesperrt' : 'Stau'} ${m.titel}${m.verzMin > 0 ? ' · +${m.verzMin} min' : ''}',
    };

/// Baustellen kommen abschnittsweise: je Name und Richtung eine Zeile.
List<Meldung> ohneDoppelte(List<Meldung> l) {
  final gesehen = <String>{};
  return [for (final m in l) if (m.art != 'baustelle' || gesehen.add('${m.titel}|${m.richtung}')) m];
}

/// Kurzfassung in der Verbindungsliste: Staus und Sperrungen einzeln, Baustellen als Anzahl.
List<String> verkehrKurz(Verbindung v) {
  if (!v.auto) return const [];
  if (!v.verkehrOk) return const ['Ohne aktuelle Verkehrslage'];
  final alle = ohneDoppelte(v.verkehr);
  final angebot = switch (v.umfahrung) {
    'stau' => ['↪ Umfahrung: spart Zeit, bleibt auf großen Straßen'],
    'sperrung' => ['↪ Umfährt die Sperrung'],
    _ => const <String>[],
  };
  final wichtig = alle.where((m) => m.art != 'baustelle').toList();
  final baustellen = alle.length - wichtig.length;
  return [
    ...angebot,
    for (final m in wichtig.take(3)) meldungText(m),
    if (wichtig.length > 3) 'und ${wichtig.length - 3} weitere Meldungen',
    if (baustellen > 0) '🚧 ${baustellen == 1 ? '1 Baustelle' : '$baustellen Baustellen'} eingerechnet',
    if (v.verkehr.isEmpty) 'Kein Stau auf der Autobahn gemeldet',
  ];
}

/// Alle Meldungen für die Detailansicht, mit Fahrtrichtung und Quelle.
class VerkehrListe extends StatelessWidget {
  final Verbindung v;
  const VerkehrListe(this.v, {super.key});

  @override
  Widget build(BuildContext context) {
    if (!v.auto || !v.verkehrOk) return const SizedBox.shrink();
    final klein = Theme.of(context).textTheme.bodySmall;
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 8, 16, 8),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        for (final m in ohneDoppelte(v.verkehr))
          Padding(
            padding: const EdgeInsets.only(bottom: 6),
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Text(meldungText(m)),
              if (m.richtung.isNotEmpty) Text(m.richtung.replaceAll('->', '→'), style: klein),
            ]),
          ),
        Text('Verkehrsdaten: Autobahn GmbH, nur Autobahnen', style: klein),
      ]),
    );
  }
}
