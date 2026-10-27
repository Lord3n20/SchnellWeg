// Ziel wählen: Haltestellen sucht das Handy in der eigenen SQLite, Adressen und Orte fragt es bei /v1/search
// (Suchtext nur im Body). Beides läuft parallel; die Haltestellen stehen oben, weil sie sofort da sind.
import 'dart:async';

import 'package:flutter/material.dart';

import 'api.dart';
import 'halte_db.dart';
import 'namen.dart';

const _warten = Duration(milliseconds: 250);
final _osmTag = RegExp(r'^[a-z_]+$'); // Orte ohne Adresse tragen als Detail nur ihren OSM-Typ („charging_station“)

class ZielSuche extends StatefulWidget {
  final Api api;
  final HalteDb db;
  const ZielSuche({super.key, required this.api, required this.db});

  @override
  State<ZielSuche> createState() => _ZielSucheState();
}

class _ZielSucheState extends State<ZielSuche> {
  final _feld = TextEditingController();
  Timer? _timer;
  int _nr = 0; // verwirft Antworten auf veraltete Eingaben
  List<Ort> _halte = [], _orte = [];
  String? _fehler;
  bool _laeuft = false;

  @override
  void dispose() {
    _timer?.cancel();
    _feld.dispose();
    super.dispose();
  }

  void _eingabe(String q) {
    _timer?.cancel();
    _timer = Timer(_warten, () => _suchen(q));
  }

  Future<void> _suchen(String q) async {
    final nr = ++_nr;
    if (q.trim().length < 2) {
      return setState(() {
        _halte = [];
        _orte = [];
        _fehler = null;
      });
    }
    setState(() => _laeuft = true);
    final halte = await widget.db.suchen(q);
    if (nr != _nr || !mounted) return;
    setState(() => _halte = _zusammenfassen(halte));
    try {
      final orte = await widget.api.suche(q);
      if (nr != _nr || !mounted) return;
      setState(() {
        _orte = orte;
        _fehler = null;
      });
    } on ApiFehler catch (e) {
      if (nr == _nr && mounted) setState(() => _fehler = 'Adresssuche nicht erreichbar ($e)');
    } finally {
      if (nr == _nr && mounted) setState(() => _laeuft = false);
    }
  }

  /// Bahnsteige gleichen Namens zu einem Treffer, Koordinate = Mittelpunkt (wie die Liste auf der Startseite).
  static List<Ort> _zusammenfassen(List<Halt> halte) {
    final gruppen = <String, List<Halt>>{};
    for (final h in halte) {
      gruppen.putIfAbsent(haltSchluessel(h.name), () => []).add(h);
    }
    return [
      for (final g in gruppen.values.take(8))
        Ort('halt', haltName(g.first.name), 'Haltestelle', g.map((h) => h.lat).reduce((a, b) => a + b) / g.length,
            g.map((h) => h.lon).reduce((a, b) => a + b) / g.length)
    ];
  }

  static IconData _icon(String art) => switch (art) {
        'halt' => Icons.directions_bus,
        'adresse' => Icons.home_outlined,
        'strasse' => Icons.add_road,
        'ort' => Icons.location_city,
        _ => Icons.place_outlined,
      };

  @override
  Widget build(BuildContext context) {
    final treffer = [..._halte, ..._orte];
    return Scaffold(
      appBar: AppBar(
        title: TextField(
          controller: _feld,
          autofocus: true,
          decoration: const InputDecoration(hintText: 'Ziel: Haltestelle, Adresse, Ort …', border: InputBorder.none),
          textInputAction: TextInputAction.search,
          onChanged: _eingabe,
          onSubmitted: _suchen,
        ),
        bottom: _laeuft
            ? const PreferredSize(preferredSize: Size.fromHeight(2), child: LinearProgressIndicator(minHeight: 2))
            : null,
      ),
      body: ListView(children: [
        if (_fehler != null) ListTile(leading: const Icon(Icons.cloud_off), title: Text(_fehler!)),
        for (final o in treffer)
          ListTile(
            leading: Icon(_icon(o.art)),
            title: Text(o.name),
            subtitle: o.detail.isEmpty || _osmTag.hasMatch(o.detail) ? null : Text(o.detail),
            onTap: () => Navigator.of(context).pop(o),
          ),
      ]),
    );
  }
}
