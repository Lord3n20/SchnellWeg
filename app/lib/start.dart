// Startseite nach docs/app-ui.md: Von/Nach, Karte, Haltestellen in der Nähe mit nächster Abfahrt.
// Abfahrten werden nur abgefragt, solange die Seite sichtbar ist (alle 30 s, kein Hintergrund-Polling).
// Ziel per Suche oder durch Antippen der Karte. Mit gewähltem Ziel zeigt die Seite statt der Haltestellen kurze Zusammenfassungen der Verbindungen.
import 'dart:async';

import 'package:flutter/material.dart';
import 'package:latlong2/latlong.dart';

import 'api.dart';
import 'details.dart';
import 'einstellungen.dart';
import 'einstellungen_seite.dart';
import 'halte_db.dart';
import 'karte.dart';
import 'kartenlager.dart';
import 'namen.dart';
import 'standort.dart';
import 'tempo.dart';
import 'zugang.dart';
import 'ziel.dart';

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
  Ort? _ziel;
  List<Verbindung>? _verbindungen; // null = wird berechnet
  bool _routeRt = false;
  String? _routeHinweis;
  int _gewaehlt = 0; // Verbindung, die die Karte zeigt
  List<Strecke> _strecken = [];
  Map<String, Halt> _halteDerVerbindung = {};
  List<LatLng> _naheHalte = [];
  double _kmh = standardKmh;

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
      if (_api != null) KartenLager.instanz.start(_api!); // prüft höchstens täglich
    } else if (s == AppLifecycleState.paused || s == AppLifecycleState.hidden) {
      _timer?.cancel();
    }
  }

  Future<void> _starten() async {
    _db = await HalteDb.oeffnen();
    _kmh = await Tempo.laden();
    final z = await Zugang.laden();
    if (z == null) return _zugangEinrichten();
    _setzeZugang(z);
  }

  void _setzeZugang(Zugang z) {
    _api?.schliessen();
    _zugang = z;
    _api = Api(z);
    _halteAbgleichen().then((_) => _aktualisieren(standortNeu: true));
    KartenLager.instanz.start(_api!);
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

  Future<void> _zielWaehlen() async {
    if (_api == null || _db == null) return;
    final o = await Navigator.of(context).push<Ort>(
        MaterialPageRoute(builder: (_) => ZielSuche(api: _api!, db: _db!)));
    if (o == null) return;
    setState(() {
      _ziel = o;
      _strecken = [];
    });
    await _routeBerechnen();
  }

  /// Angetippter Kartenpunkt als Ziel; benannt nach der nächsten Haltestelle, weil es keine Rückwärtssuche gibt.
  Future<void> _zielAusKarte(LatLng p) async {
    if (_api == null || _db == null) return;
    final nah = await _db!.naechste(p.latitude, p.longitude, radius: 300);
    final name = nah.isEmpty ? 'Punkt auf der Karte' : 'Punkt bei ${haltName(nah.first.$1.name)}';
    if (!mounted) return;
    setState(() {
      _ziel = Ort('karte', name, '', p.latitude, p.longitude);
      _strecken = [];
    });
    await _routeBerechnen();
  }

  void _zielWeg() => setState(() {
        _ziel = null;
        _verbindungen = null;
        _routeHinweis = null;
        _strecken = [];
      });

  /// Verbindung [i] als Linien für die Karte: Halte aus der SQLite, Start/Ziel aus Standort und Suchtreffer.
  /// Fußwege folgen der Wegform vom Server (sonst gerade), Fahrten verbinden ihre Halte der Reihe nach
  /// (Linienverlauf liefert keiner der Feeds).
  Future<void> _waehlen(int i) async {
    final v = _verbindungen, ziel = _ziel, pos = _pos;
    if (v == null || i >= v.length || ziel == null || pos == null) return;
    final halte = await _db!.holen([
      for (final a in v[i].abschnitte) ...[?a.von?.id, ?a.nach?.id, ...a.halte]
    ]);
    LatLng? punkt(HaltRef? h, LatLng sonst) {
      if (h == null) return sonst;
      final x = halte[h.id];
      return x == null ? null : LatLng(x.lat, x.lon);
    }

    final start = LatLng(pos.lat, pos.lon), ende = LatLng(ziel.lat, ziel.lon);
    final strecken = <Strecke>[];
    for (final a in v[i].abschnitte) {
      final von = punkt(a.von, start), nach = punkt(a.nach, ende);
      if (von == null || nach == null || von == nach) continue;
      if (a.fahrt) {
        final unterwegs = [for (final id in a.halte) ?halte[id]].map((h) => LatLng(h.lat, h.lon)).toList();
        final punkte = unterwegs.length >= 2 ? unterwegs : [von, nach];
        strecken.add(Strecke(punkte, false, a.linie,
            zwischen: punkte.length > 2 ? punkte.sublist(1, punkte.length - 1) : const []));
      } else {
        final form = [for (final (lat, lon) in polyline(a.form)) LatLng(lat, lon)];
        strecken.add(Strecke([von, ...form, nach], true, ''));
      }
    }
    if (!mounted || _ziel != ziel) return;
    setState(() {
      _gewaehlt = i;
      _strecken = strecken;
      _halteDerVerbindung = halte;
    });
  }

  /// Verbindung auf der Karte zeigen und die Detailseite öffnen.
  Future<void> _details(int i) async {
    await _waehlen(i);
    final v = _verbindungen, ziel = _ziel;
    if (!mounted || v == null || ziel == null || _gewaehlt != i) return;
    await Navigator.of(context).push(MaterialPageRoute(
      builder: (_) => Details(
        v: v[i],
        halte: _halteDerVerbindung,
        strecken: _strecken,
        zielName: ziel.name,
        standort: _pos == null ? null : LatLng(_pos!.lat, _pos!.lon),
        ziel: LatLng(ziel.lat, ziel.lon),
      ),
    ));
  }

  Future<void> _routeBerechnen() async {
    final ziel = _ziel;
    if (ziel == null || _api == null) return;
    setState(() {
      _verbindungen = null;
      _routeHinweis = null;
    });
    final p = await Standort.holen() ?? _pos;
    if (p == null) return setState(() => _routeHinweis = 'Kein Standort (Berechtigung oder GPS aus?)');
    _pos = p;
    try {
      final (v, rt) = await _api!.route(p.lat, p.lon, ziel.lat, ziel.lon, kmh: _kmh);
      if (!mounted || _ziel != ziel) return;
      setState(() {
        _verbindungen = v;
        _routeRt = rt;
        if (v.isEmpty) _routeHinweis = 'Keine Verbindung gefunden';
      });
      await _waehlen(0);
    } on ApiFehler catch (e) {
      if (!mounted || _ziel != ziel) return;
      setState(() {
        _verbindungen = [];
        _routeHinweis = switch (e.art) {
          'keine_haltestelle_in_reichweite' => 'Keine Haltestelle in Gehweite von Start oder Ziel',
          'anderer_betriebstag' => 'Fahrplan für diesen Tag noch nicht geladen',
          'fahrplan_laedt' => 'Server lädt gerade den Fahrplan',
          _ => 'Route nicht berechnet ($e)',
        };
      });
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
    _naheHalte = [for (final (h, _) in halte.take(40)) LatLng(h.lat, h.lon)];
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
        actions: [
          IconButton(
            icon: const Icon(Icons.settings),
            tooltip: 'Einstellungen',
            onPressed: () => Navigator.of(context).push(MaterialPageRoute(
              builder: (_) => EinstellungenSeite(
                kmh: _kmh,
                zugang: _zugang,
                tempoGeaendert: (k) {
                  setState(() => _kmh = k);
                  _routeBerechnen();
                },
                zugangGeaendert: _setzeZugang,
              ),
            )),
          ),
        ],
      ),
      body: Column(children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(12, 4, 12, 8),
          child: Column(children: [
            _Feld(label: 'Von', text: 'Mein Standort', icon: Icons.my_location, onIcon: () => _aktualisieren(standortNeu: true)),
            const SizedBox(height: 8),
            _Feld(
              label: 'Nach',
              text: _ziel?.name ?? 'Ziel …',
              leer: _ziel == null,
              onTap: _zielWaehlen,
              icon: _ziel == null ? null : Icons.close,
              onIcon: _zielWeg,
            ),
          ]),
        ),
        Expanded(
          flex: 5,
          child: Karte(
            standort: _pos == null ? null : LatLng(_pos!.lat, _pos!.lon),
            ziel: _ziel == null ? null : LatLng(_ziel!.lat, _ziel!.lon),
            halte: _naheHalte,
            strecken: _strecken,
            onAntippen: _zielAusKarte,
          ),
        ),
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 12, 16, 4),
          child: Row(children: [
            Text(_ziel == null ? 'Haltestellen in der Nähe' : 'Verbindungen',
                style: Theme.of(context).textTheme.titleSmall),
            const Spacer(),
            if (_ziel == null ? _gruppen.isNotEmpty && !_rt : (_verbindungen?.isNotEmpty ?? false) && !_routeRt)
              Text('nur Fahrplan', style: TextStyle(fontSize: 12, color: farbe.outline)),
          ]),
        ),
        Expanded(flex: 6, child: _ziel == null ? _halteListe() : _routeListe()),
      ]),
    );
  }

  Widget _halteListe() => _hinweis != null && _gruppen.every((g) => g.abfahrten.isEmpty)
      ? Center(child: Text(_hinweis!))
      : RefreshIndicator(
          onRefresh: () => _aktualisieren(standortNeu: true),
          child: ListView(children: [for (final g in _gruppen) _HaltZeile(g)]),
        );

  Widget _routeListe() {
    final v = _verbindungen;
    if (v == null) return const Center(child: CircularProgressIndicator());
    if (v.isEmpty) return Center(child: Text(_routeHinweis ?? 'Keine Verbindung gefunden'));
    return RefreshIndicator(
      onRefresh: _routeBerechnen,
      child: ListView(children: [
        for (final (i, x) in v.indexed) _VerbindungZeile(x, gewaehlt: i == _gewaehlt, onTap: () => _details(i)),
      ]),
    );
  }
}

class _Feld extends StatelessWidget {
  final String label, text;
  final IconData? icon;
  final VoidCallback? onIcon, onTap;
  final bool leer; // Platzhaltertext blass zeigen
  const _Feld({required this.label, required this.text, this.icon, this.onIcon, this.onTap, this.leer = false});

  @override
  Widget build(BuildContext context) => InkWell(
        onTap: onTap,
        borderRadius: BorderRadius.circular(4),
        child: InputDecorator(
          decoration: InputDecoration(
            labelText: label,
            border: const OutlineInputBorder(),
            isDense: true,
            suffixIcon: icon == null ? null : IconButton(icon: Icon(icon), onPressed: onIcon),
          ),
          child: Text(text,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: leer ? TextStyle(color: Theme.of(context).colorScheme.outline) : null),
        ),
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

String _uhr(DateTime t) => '${t.hour.toString().padLeft(2, '0')}:${t.minute.toString().padLeft(2, '0')}';

/// Kurzfassung nach app-ui.md: 🚶 4 min → 33 Elisenbrunnen 12:41 (+1) → 🚶 2 min → an 13:02 · 1 Umstieg
class _VerbindungZeile extends StatelessWidget {
  final Verbindung v;
  final bool gewaehlt;
  final VoidCallback onTap;
  const _VerbindungZeile(this.v, {required this.gewaehlt, required this.onTap});

  @override
  Widget build(BuildContext context) {
    final farbe = Theme.of(context).colorScheme;
    final teile = <InlineSpan>[];
    void pfeil() {
      if (teile.isNotEmpty) teile.add(TextSpan(text: '  →  ', style: TextStyle(color: farbe.outline)));
    }

    for (final a in v.abschnitte) {
      final min = (a.an.difference(a.ab).inSeconds / 60).round();
      if (!a.fahrt) {
        if (min < 1) continue;
        pfeil();
        teile.add(TextSpan(text: '🚶 $min min'));
        continue;
      }
      pfeil();
      teile.add(TextSpan(
          text: a.linie,
          style: TextStyle(
              fontWeight: FontWeight.w700, color: a.faelltAus || a.entfaellt ? farbe.error : null,
              decoration: a.faelltAus ? TextDecoration.lineThrough : null)));
      teile.add(TextSpan(text: ' ${haltName(a.von?.name ?? '')} ${_uhr(a.ab)}'));
      final vs = a.versp ?? 0;
      if (vs != 0) {
        teile.add(TextSpan(
            text: ' (${vs > 0 ? '+' : ''}$vs)', style: TextStyle(color: vs > 0 ? farbe.error : farbe.primary)));
      }
      if (a.fern) teile.add(TextSpan(text: ' €', style: TextStyle(color: farbe.tertiary)));
      if (a.rad) teile.add(TextSpan(text: ' 🚲€', style: TextStyle(color: farbe.tertiary)));
      if (a.risiko) teile.add(TextSpan(text: ' ⚠', style: TextStyle(color: farbe.error)));
    }

    final umstiege = v.fahrten - 1;
    final kopf = [
      '${_uhr(v.ab)} – ${_uhr(v.ankunft)}',
      '${v.ankunft.difference(v.ab).inMinutes} min',
      switch (umstiege) { -1 => 'nur zu Fuß', 0 => 'direkt', 1 => '1 Umstieg', _ => '$umstiege Umstiege' },
    ].join(' · ');
    final warnung = switch (v.risiko) {
      'anschluss' => 'Anschluss gefährdet',
      'ausfall' => 'Fahrt fällt aus',
      _ => null,
    };

    return Card(
      margin: const EdgeInsets.symmetric(horizontal: 12, vertical: 4),
      clipBehavior: Clip.antiAlias,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(12),
        side: BorderSide(color: gewaehlt ? farbe.primary : Colors.transparent, width: 2),
      ),
      child: InkWell(onTap: onTap, child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Row(children: [
            Expanded(child: Text(kopf, style: const TextStyle(fontWeight: FontWeight.w600))),
            if (v.anRt != null && _uhr(v.anRt!) != _uhr(v.an))
              Text('statt ${_uhr(v.an)}', style: TextStyle(fontSize: 12, color: farbe.outline)),
          ]),
          const SizedBox(height: 4),
          Text.rich(TextSpan(children: teile)),
          if (warnung != null)
            Padding(
              padding: const EdgeInsets.only(top: 4),
              child: Text(warnung, style: TextStyle(color: farbe.error, fontWeight: FontWeight.w600)),
            ),
        ]),
      )),
    );
  }
}
