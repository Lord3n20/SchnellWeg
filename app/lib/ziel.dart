// Ziel wählen: Haltestellen sucht das Handy in der eigenen SQLite, Adressen und Orte fragt es bei /v1/search
// (Suchtext nur im Body), RWTH-Gebäude live bei RWTH Maps (D-18). Alles parallel; Haltestellen oben, weil sofort da.
// Ohne Eingabe stehen die Favoriten da (Ordner aufklappbar); jeder Treffer hat einen Stern.
import 'dart:async';

import 'package:flutter/material.dart';

import 'api.dart';
import 'favoriten.dart';
import 'halte_db.dart';
import 'namen.dart';
import 'rwth.dart';
import 'standort.dart';

const _warten = Duration(milliseconds: 250);
final _osmTag = RegExp(r'^[a-z_]+$'); // Orte ohne Adresse tragen als Detail nur ihren OSM-Typ („charging_station“)

class ZielSuche extends StatefulWidget {
  final Api api;
  final HalteDb db;
  final Position? nahe; // schon bekannter Standort der Startseite (spart den GPS-Start)
  const ZielSuche({super.key, required this.api, required this.db, this.nahe});

  @override
  State<ZielSuche> createState() => _ZielSucheState();
}

class _ZielSucheState extends State<ZielSuche> {
  final _feld = TextEditingController();
  Timer? _timer;
  int _nr = 0; // verwirft Antworten auf veraltete Eingaben
  List<Ort> _halte = [], _orte = [], _gebaeude = [];
  String? _fehler;
  bool _laeuft = false;
  Position? _nahe;

  @override
  void initState() {
    super.initState();
    Favoriten.instanz.laden();
    _nahe = widget.nahe; // für die Adresssuche: nahe Treffer zuerst
    if (_nahe == null) Standort.holen().then((p) => _nahe = p);
  }

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
        _gebaeude = [];
        _fehler = null;
      });
    }
    setState(() => _laeuft = true);
    // Alle drei gleichzeitig: die Server-Suche wartet nicht mehr auf die SQLite
    widget.db.suchen(q).then((halte) {
      if (nr == _nr && mounted) setState(() => _halte = _zusammenfassen(halte));
    });
    rwthSuche(q).then((g) {
      if (nr == _nr && mounted) setState(() => _gebaeude = g);
    });
    try {
      final orte = await widget.api.suche(q, nahe: _nahe);
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
        'gebaeude' => Icons.school_outlined,
        'adresse' => Icons.home_outlined,
        'strasse' => Icons.add_road,
        'ort' => Icons.location_city,
        _ => Icons.place_outlined,
      };

  Widget _favZeile(Ort o) => ListTile(
        leading: Icon(Icons.star, color: Colors.amber.shade700),
        title: Text(o.name),
        subtitle: o.detail.isEmpty || _osmTag.hasMatch(o.detail) ? null : Text(o.detail),
        onTap: () => Navigator.of(context).pop(o),
        onLongPress: () => favoritBearbeiten(context, o),
      );

  @override
  Widget build(BuildContext context) {
    // Gebäude vom Server nur, wenn die Live-Abfrage sie nicht schon hat (oder ausgefallen ist)
    final live = {for (final g in _gebaeude) g.name};
    final orte = _orte.where((o) => o.art != 'gebaeude' || !live.contains(o.name));
    final nachNr = RegExp(r'^\d{3,4}[a-zA-Z]?$').hasMatch(_feld.text.trim()); // „1010“: Gebäude zuerst
    final treffer = nachNr ? [..._gebaeude, ..._halte, ...orte] : [..._halte, ..._gebaeude, ...orte];
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
        // Immer dieselbe Leiste, nur der Balken wechselt: ein wechselndes `bottom` baut das Suchfeld neu auf, dann
        // verbindet sich die Tastatur neu und springt z. B. von den Ziffern zurück auf Buchstaben.
        bottom: PreferredSize(
          preferredSize: const Size.fromHeight(2),
          child: _laeuft ? const LinearProgressIndicator(minHeight: 2) : const SizedBox(height: 2),
        ),
      ),
      body: ListenableBuilder(
        listenable: Favoriten.instanz,
        builder: (context, _) => ListView(children: [
          if (_fehler != null) ListTile(leading: const Icon(Icons.cloud_off), title: Text(_fehler!)),
          if (_feld.text.trim().length < 2) ...[
            for (final o in Favoriten.instanz.oben) _favZeile(o),
            for (final (name, orte) in Favoriten.instanz.ordner)
              ExpansionTile(
                leading: const Icon(Icons.folder_outlined),
                title: Text(name),
                subtitle: Text(orte.length == 1 ? '1 Favorit' : '${orte.length} Favoriten'),
                childrenPadding: const EdgeInsets.only(left: 24),
                children: [for (final o in orte) _favZeile(o)],
              ),
            if (Favoriten.instanz.eintraege.isNotEmpty)
              ListTile(
                leading: const Icon(Icons.swap_vert),
                title: const Text('Favoriten ordnen'),
                onTap: () => favoritenOrdnen(context),
              ),
          ],
          for (final o in treffer)
            ListTile(
              leading: Icon(_icon(o.art)),
              title: Text(o.name),
              subtitle: o.detail.isEmpty || _osmTag.hasMatch(o.detail) ? null : Text(o.detail),
              trailing: FavoritStern(o),
              onTap: () => Navigator.of(context).pop(o),
            ),
        ]),
      ),
    );
  }
}
