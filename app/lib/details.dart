// Detailansicht einer Verbindung: Karte mit genau dieser Verbindung, darunter Schritt für Schritt
// Fußwege (Dauer, Länge) und Fahrten (Einstieg mit Bahnsteig, alle Halte unterwegs, Ausstieg hervorgehoben).
// Halt-Namen kommen aus der lokalen SQLite; Zeiten je Zwischenhalt liefert der Server nicht.
// Die Glocke oben startet die Ausstiegs-Erinnerung (aussteigen.dart).
import 'package:flutter/material.dart';
import 'package:latlong2/latlong.dart';

import 'api.dart';
import 'aussteigen.dart';
import 'halte_db.dart';
import 'karte.dart';
import 'namen.dart';

String _uhr(DateTime t) => '${t.hour.toString().padLeft(2, '0')}:${t.minute.toString().padLeft(2, '0')}';

class Details extends StatelessWidget {
  final Verbindung v;
  final Map<String, Halt> halte; // alle Halte der Verbindung aus der SQLite
  final List<Strecke> strecken;
  final String zielName;
  final LatLng? standort, ziel;
  const Details({
    super.key,
    required this.v,
    required this.halte,
    required this.strecken,
    required this.zielName,
    required this.standort,
    required this.ziel,
  });

  @override
  Widget build(BuildContext context) {
    final umstiege = v.fahrten - 1;
    return Scaffold(
      appBar: AppBar(
        title: Text('${_uhr(v.ab)} – ${_uhr(v.ankunft)} · ${v.ankunft.difference(v.ab).inMinutes} min'),
        actions: [if (v.fahrten > 0) ErinnerungKnopf(v: v, halte: halte)],
        bottom: PreferredSize(
          preferredSize: const Size.fromHeight(20),
          child: Padding(
            padding: const EdgeInsets.only(left: 16, bottom: 6),
            child: Align(
              alignment: Alignment.centerLeft,
              child: Text(
                switch (umstiege) { -1 => 'nur zu Fuß', 0 => 'direkt', 1 => '1 Umstieg', _ => '$umstiege Umstiege' },
                style: Theme.of(context).textTheme.bodySmall,
              ),
            ),
          ),
        ),
      ),
      body: Column(children: [
        Expanded(
          flex: 4,
          child: Karte(standort: standort, ziel: ziel, strecken: strecken),
        ),
        Expanded(
          flex: 6,
          child: ListView(padding: const EdgeInsets.symmetric(vertical: 8), children: [
            for (final (i, a) in v.abschnitte.indexed)
              if (a.fahrt)
                _Fahrt(a, halte)
              else if (a.an.difference(a.ab).inSeconds >= 30 || a.form.isNotEmpty)
                _Fuss(a, ziel: i == v.abschnitte.length - 1 ? zielName : null),
            _Zeile(
              icon: Icons.place,
              farbe: Theme.of(context).colorScheme.error,
              titel: 'Ankunft $zielName',
              zeit: _uhr(v.ankunft),
              fett: true,
            ),
          ]),
        ),
      ]),
    );
  }
}

/// Bahnsteig in Klammern hinter dem Namen, wenn der Feed einen trägt.
String _mitSteig(HaltRef h) {
  final s = bahnsteig(h.name);
  return s == null ? haltName(h.name) : '${haltName(h.name)}  ·  Steig $s';
}

class _Fuss extends StatelessWidget {
  final Abschnitt a;
  final String? ziel; // letzter Fußweg: zum Ziel statt zur Haltestelle
  const _Fuss(this.a, {this.ziel});

  @override
  Widget build(BuildContext context) {
    final min = (a.an.difference(a.ab).inSeconds / 60).round();
    final punkte = polyline(a.form);
    var meter = 0.0;
    for (var i = 1; i < punkte.length; i++) {
      meter += entfernung(punkte[i - 1].$1, punkte[i - 1].$2, punkte[i].$1, punkte[i].$2);
    }
    final wohin = a.nach != null ? 'zu ${_mitSteig(a.nach!)}' : 'zu ${ziel ?? 'Ziel'}';
    final laenge = meter > 0 ? ' · ${meter < 1000 ? '${(meter / 10).round() * 10} m' : '${(meter / 100).round() / 10} km'.replaceAll('.', ',')}' : '';
    return _Zeile(
      icon: Icons.directions_walk,
      farbe: Theme.of(context).colorScheme.outline,
      titel: 'Gehen $wohin',
      unter: '${min < 1 ? '< 1' : min} min$laenge',
      zeit: _uhr(a.ab),
    );
  }
}

class _Fahrt extends StatefulWidget {
  final Abschnitt a;
  final Map<String, Halt> halte;
  const _Fahrt(this.a, this.halte);

  @override
  State<_Fahrt> createState() => _FahrtState();
}

class _FahrtState extends State<_Fahrt> {
  bool _offen = false;

  @override
  Widget build(BuildContext context) {
    final a = widget.a, farbe = Theme.of(context).colorScheme, linie = linienFarbe(a.linie);
    // Halte zwischen Ein- und Ausstieg (Server liefert Einstieg … Ausstieg)
    final unterwegs = a.halte.length > 2 ? a.halte.sublist(1, a.halte.length - 1) : const <String>[];
    final namen = [for (final id in unterwegs) haltName(widget.halte[id]?.name ?? '?')];
    final anzahl = a.halte.length >= 2 ? a.halte.length - 1 : null; // Halte bis zum Ausstieg
    final vs = a.versp ?? 0;
    final vsAn = a.anRt == null ? 0 : (a.anRt!.difference(a.an).inSeconds / 60).round();

    String verspText(int m) => m == 0 ? '' : ' (${m > 0 ? '+' : ''}$m)';

    return Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
      _Zeile(
        icon: Icons.directions_bus,
        farbe: linie,
        titel: 'Einsteigen ${a.von == null ? '' : _mitSteig(a.von!)}',
        zeit: '${_uhr(a.ab)}${verspText(vs)}',
        zeitFarbe: vs > 0 ? farbe.error : (vs < 0 ? farbe.primary : null),
        fett: true,
      ),
      Padding(
        padding: const EdgeInsets.only(left: 56, right: 16),
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Text.rich(TextSpan(children: [
            WidgetSpan(
              alignment: PlaceholderAlignment.middle,
              child: Container(
                padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 1),
                decoration: BoxDecoration(color: linie, borderRadius: BorderRadius.circular(4)),
                child: Text(a.linie, style: const TextStyle(color: Colors.white, fontWeight: FontWeight.w700)),
              ),
            ),
            TextSpan(text: '  Richtung ${zielName(a.ziel)}'),
            if (a.fern) TextSpan(text: '  · Fernverkehr €', style: TextStyle(color: farbe.tertiary)),
            if (a.rad) TextSpan(text: '  · Fahrradticket nötig', style: TextStyle(color: farbe.tertiary)),
          ])),
          if (a.faelltAus) Text('Fahrt fällt aus', style: TextStyle(color: farbe.error, fontWeight: FontWeight.w600)),
          if (a.entfaellt) Text('Halt entfällt', style: TextStyle(color: farbe.error, fontWeight: FontWeight.w600)),
          if (a.risiko) Text('Anschluss gefährdet', style: TextStyle(color: farbe.error, fontWeight: FontWeight.w600)),
          if (anzahl != null)
            InkWell(
              onTap: namen.isEmpty ? null : () => setState(() => _offen = !_offen),
              child: Padding(
                padding: const EdgeInsets.symmetric(vertical: 6),
                child: Row(children: [
                  Text('$anzahl ${anzahl == 1 ? 'Halt' : 'Halte'} · ${a.an.difference(a.ab).inMinutes} min',
                      style: TextStyle(color: farbe.outline)),
                  if (namen.isNotEmpty) Icon(_offen ? Icons.expand_less : Icons.expand_more, color: farbe.outline),
                ]),
              ),
            ),
          if (_offen)
            for (final n in namen)
              Padding(
                padding: const EdgeInsets.only(bottom: 4),
                child: Row(children: [
                  Container(width: 8, height: 8, decoration: BoxDecoration(shape: BoxShape.circle, border: Border.all(color: linie, width: 2))),
                  const SizedBox(width: 8),
                  Expanded(child: Text(n)),
                ]),
              ),
          if (!_offen && namen.isNotEmpty)
            Text('davor: ${namen.last}', style: TextStyle(fontSize: 13, color: farbe.onSurfaceVariant)),
        ]),
      ),
      _Zeile(
        icon: Icons.logout,
        farbe: linie,
        titel: 'Aussteigen ${a.nach == null ? '' : _mitSteig(a.nach!)}',
        zeit: '${_uhr(a.an)}${verspText(vsAn)}',
        zeitFarbe: vsAn > 0 ? farbe.error : (vsAn < 0 ? farbe.primary : null),
        fett: true,
      ),
    ]);
  }
}

class _Zeile extends StatelessWidget {
  final IconData icon;
  final Color farbe;
  final String titel, zeit;
  final String? unter;
  final Color? zeitFarbe;
  final bool fett;
  const _Zeile({
    required this.icon,
    required this.farbe,
    required this.titel,
    required this.zeit,
    this.unter,
    this.zeitFarbe,
    this.fett = false,
  });

  @override
  Widget build(BuildContext context) => ListTile(
        dense: true,
        leading: Icon(icon, color: farbe),
        title: Text(titel, style: fett ? const TextStyle(fontWeight: FontWeight.w600) : null),
        subtitle: unter == null ? null : Text(unter!),
        trailing: Text(zeit, style: TextStyle(fontWeight: FontWeight.w600, color: zeitFarbe)),
      );
}
