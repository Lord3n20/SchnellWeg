// Startseite nach docs/app-ui.md: Von/Nach, Karte, Haltestellen in der Nähe mit nächster Abfahrt.
// Abfahrten werden nur abgefragt, solange die Seite sichtbar ist (alle 30 s, kein Hintergrund-Polling).
import 'dart:async';

import 'package:flutter/material.dart';

import 'api.dart';
import 'einstellungen.dart';
import 'halte_db.dart';
import 'namen.dart';
import 'standort.dart';
import 'zugang.dart';

const _takt = Duration(seconds: 30);
const _maxNamen = 5; // so viele Haltestellen (nach Name zusammengefasst) zeigt die Liste
const _maxIds = 30; // Grenze von /v1/departures

class _Gruppe {
  final String name;
  final double meter;
  final List<String> ids;
  List<Abfahrt> abfahrten = [];
  _Gruppe(this.name, this.meter, this.ids);
}

class Start extends StatefulWidget {
  const Start({super.key});

  @override
  State<Start> createState() => _StartState();
}

class _StartState extends State<Start> with WidgetsBindingObserver {
  Zugang? _zugang;
  Api? _api;
  HalteDb? _db;
  Timer? _timer;
  Position? _pos;
  List<_Gruppe> _gruppen = [];
  bool _rt = false;
  String? _hinweis;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _starten();
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _timer?.cancel();
    _api?.schliessen();
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState s) {
    if (s == AppLifecycleState.resumed) {
      _aktualisieren(standortNeu: true);
    } else if (s == AppLifecycleState.paused || s == AppLifecycleState.hidden) {
      _timer?.cancel();
    }
  }

  Future<void> _starten() async {
    _db = await HalteDb.oeffnen();
    final z = await Zugang.laden();
    if (z == null) return _zugangEinrichten();
    _setzeZugang(z);
  }

  void _setzeZugang(Zugang z) {
    _api?.schliessen();
    _zugang = z;
    _api = Api(z);
    _halteAbgleichen().then((_) => _aktualisieren(standortNeu: true));
  }

  Future<void> _zugangEinrichten() async {
    final z = await Navigator.of(context).push<Zugang>(MaterialPageRoute(builder: (_) => Einstellungen(alt: _zugang)));
    if (z != null) _setzeZugang(z);
  }

  /// Haltestellenliste vom Server holen, wenn sie sich geändert hat (ETag = Fahrplanstand).
  Future<void> _halteAbgleichen() async {
    final db = _db!, api = _api!;
    try {
      final neu = await api.halte(await db.etag());
      if (neu != null) await db.ersetzen(neu.$1, neu.$2);
    } on ApiFehler catch (e) {
      if (await db.anzahl() == 0) setState(() => _hinweis = 'Haltestellen nicht geladen ($e)');
    }
  }

  Future<void> _aktualisieren({bool standortNeu = false}) async {
    _timer?.cancel();
    if (_api == null || !mounted) return;
    if (standortNeu || _pos == null) {
      final p = await Standort.holen();
      if (p == null && _pos == null) return setState(() => _hinweis = 'Kein Standort (Berechtigung oder GPS aus?)');
      _pos = p ?? _pos;
      _gruppen = await _gruppieren(_pos!);
    }
    final ids = [for (final g in _gruppen) ...g.ids].take(_maxIds).toList();
    if (ids.isNotEmpty) {
      try {
        final (abf, rt) = await _api!.abfahrten(ids);
        for (final g in _gruppen) {
          g.abfahrten = [for (final id in g.ids) ...?abf[id]]..sort((a, b) => a.erwartet.compareTo(b.erwartet));
        }
        _rt = rt;
        _hinweis = null;
      } on ApiFehler catch (e) {
        _hinweis = 'Abfahrten nicht geladen ($e)';
      }
    } else {
      _hinweis = 'Keine Haltestelle im Umkreis';
    }
    if (!mounted) return;
    setState(() {});
    _timer = Timer(_takt, _aktualisieren);
  }

  /// Bahnsteige gleichen Namens zu einer Haltestelle zusammenfassen; Entfernung = nächster Bahnsteig.
  Future<List<_Gruppe>> _gruppieren(Position p) async {
    final halte = await _db!.naechste(p.lat, p.lon);
    final gruppen = <String, _Gruppe>{};
    for (final (h, m) in halte) {
      final k = haltSchluessel(h.name);
      final g = gruppen[k];
      if (g != null) {
        g.ids.add(h.id);
      } else if (gruppen.length < _maxNamen) {
        gruppen[k] = _Gruppe(haltName(h.name), m, [h.id]);
      }
    }
    return gruppen.values.toList();
  }

  @override
  Widget build(BuildContext context) {
    final farbe = Theme.of(context).colorScheme;
    return Scaffold(
      appBar: AppBar(
        title: const Text('SchnellWeg'),
        actions: [IconButton(icon: const Icon(Icons.settings), tooltip: 'Zugang', onPressed: _zugangEinrichten)],
      ),
      body: Column(children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(12, 4, 12, 8),
          child: Column(children: [
            _Feld(label: 'Von', text: 'Mein Standort', icon: Icons.my_location, onIcon: () => _aktualisieren(standortNeu: true)),
            const SizedBox(height: 8),
            const _Feld(label: 'Nach', text: 'Ziel …'),
          ]),
        ),
        Expanded(
          flex: 5,
          child: Container(
            color: farbe.surfaceContainerHighest,
            alignment: Alignment.center,
            child: Text('Karte', style: TextStyle(color: farbe.onSurfaceVariant)),
          ),
        ),
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 12, 16, 4),
          child: Row(children: [
            Text('Haltestellen in der Nähe', style: Theme.of(context).textTheme.titleSmall),
            const Spacer(),
            if (_gruppen.isNotEmpty && !_rt) Text('nur Fahrplan', style: TextStyle(fontSize: 12, color: farbe.outline)),
          ]),
        ),
        Expanded(
          flex: 6,
          child: _hinweis != null && _gruppen.every((g) => g.abfahrten.isEmpty)
              ? Center(child: Text(_hinweis!))
              : RefreshIndicator(
                  onRefresh: () => _aktualisieren(standortNeu: true),
                  child: ListView(children: [for (final g in _gruppen) _HaltZeile(g)]),
                ),
        ),
      ]),
    );
  }
}

class _Feld extends StatelessWidget {
  final String label, text;
  final IconData? icon;
  final VoidCallback? onIcon;
  const _Feld({required this.label, required this.text, this.icon, this.onIcon});

  @override
  Widget build(BuildContext context) => InputDecorator(
        decoration: InputDecoration(
          labelText: label,
          border: const OutlineInputBorder(),
          isDense: true,
          suffixIcon: icon == null ? null : IconButton(icon: Icon(icon), onPressed: onIcon),
        ),
        child: Text(text),
      );
}

class _HaltZeile extends StatelessWidget {
  final _Gruppe g;
  const _HaltZeile(this.g);

  @override
  Widget build(BuildContext context) {
    final farbe = Theme.of(context).colorScheme;
    final jetzt = DateTime.now();
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 6),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Row(children: [
          Expanded(child: Text(g.name, style: const TextStyle(fontWeight: FontWeight.w600))),
          Text('${(g.meter / 10).round() * 10} m', style: TextStyle(color: farbe.outline)),
        ]),
        for (final a in g.abfahrten.take(3))
          Padding(
            padding: const EdgeInsets.only(left: 12, top: 2),
            child: Text.rich(TextSpan(children: [
              TextSpan(text: a.line, style: const TextStyle(fontWeight: FontWeight.w600)),
              TextSpan(text: ' → ${zielName(a.headsign)}   '),
              if (a.canceled)
                TextSpan(text: 'fällt aus', style: TextStyle(color: farbe.error))
              else ...[
                TextSpan(text: _wann(a.erwartet, jetzt)),
                if ((a.versp ?? 0) != 0)
                  TextSpan(
                    text: ' (${a.versp! > 0 ? '+' : ''}${a.versp})',
                    style: TextStyle(color: a.versp! > 0 ? farbe.error : farbe.primary),
                  ),
              ],
            ])),
          ),
      ]),
    );
  }

  static String _wann(DateTime t, DateTime jetzt) {
    final min = t.difference(jetzt).inSeconds / 60;
    if (min < 0.5) return 'jetzt';
    if (min < 60) return 'in ${min.round()} min';
    return '${t.hour.toString().padLeft(2, '0')}:${t.minute.toString().padLeft(2, '0')}';
  }
}
