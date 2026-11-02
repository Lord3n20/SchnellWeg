// Startseite nach docs/app-ui.md: Von/Nach, Karte, Haltestellen in der Nähe mit nächster Abfahrt.
// Abfahrten werden nur abgefragt, solange die Seite sichtbar ist (alle 30 s, kein Hintergrund-Polling; auch nicht,
// solange eine Unterseite sie verdeckt).
// Ziel per Suche oder durch Antippen der Karte; eine angetippte Haltestelle (Karte oder Liste) öffnet ihre Abfahrten. Mit gewähltem Ziel zeigt die Seite statt der Haltestellen kurze Zusammenfassungen der Verbindungen.
import 'dart:async';

import 'package:flutter/material.dart';
import 'package:latlong2/latlong.dart';

import 'abfahrten.dart';
import 'abweichung.dart';
import 'api.dart';
import 'details.dart';
import 'einstellungen.dart';
import 'einstellungen_seite.dart';
import 'favoriten.dart';
import 'halte_db.dart';
import 'karte.dart';
import 'kartenlager.dart';
import 'losgehen.dart';
import 'namen.dart';
import 'standort.dart';
import 'tempo.dart';
import 'zugang.dart';
import 'ziel.dart';

const _takt = Duration(seconds: 30);

/// Meldet der Startseite, wann eine Unterseite sie verdeckt (dann ruht der Abfahrten-Takt); Dialoge zählen nicht.
final seitenBeobachter = RouteObserver<PageRoute<dynamic>>();
const _maxNamen = 5; // so viele Haltestellen (nach Name zusammengefasst) zeigt die Liste
const _maxIds = 30; // Grenze von /v1/departures

/// Oberste Zeile der Haltestellenliste: nächster Termin mit Ort und wann man los muss.
class _TerminZeile extends StatelessWidget {
  final Losgehplan p;
  final VoidCallback onTap;
  const _TerminZeile(this.p, {required this.onTap});

  @override
  Widget build(BuildContext context) => ListTile(
        leading: Icon(Icons.event, color: Theme.of(context).colorScheme.primary),
        title: Text('${_uhr(p.termin.beginn)} ${p.termin.titel}', maxLines: 1, overflow: TextOverflow.ellipsis),
        subtitle: Text(losText(p), maxLines: 2),
        onTap: p.ziel == null ? null : onTap,
      );
}

class _Gruppe {
  final String name;
  final double meter;
  final List<String> ids;
  final Halt naechster;
  List<Abfahrt> abfahrten = [];
  _Gruppe(this.name, this.meter, this.ids, this.naechster);
}

/// Angetippte Haltestelle: ersetzt unten die Liste bzw. die Verbindungen, die Karte bleibt sichtbar.
const _neuAbstand = Duration(seconds: 15); // höchstens so oft neu berechnen, wenn man abgewichen ist

class _GewaehlterHalt {
  final Ort ort;
  final List<String> ids;
  List<Abfahrt>? abfahrten;
  bool rt = true;
  String? fehler;
  _GewaehlterHalt(this.ort, this.ids);
}

class Start extends StatefulWidget {
  const Start({super.key});

  @override
  State<Start> createState() => _StartState();
}

class _StartState extends State<Start> with WidgetsBindingObserver, RouteAware {
  Zugang? _zugang;
  Api? _api;
  HalteDb? _db;
  Timer? _timer;
  bool _verdeckt = false; // eine Unterseite liegt darüber
  Position? _pos;
  List<_Gruppe> _gruppen = [];
  bool _rt = false;
  String? _hinweis;
  Ort? _ziel;
  _GewaehlterHalt? _halt;
  List<Verbindung>? _verbindungen; // null = wird berechnet
  bool _routeRt = false;
  String? _routeHinweis;
  bool _langFrage = false; // Hinweis ist eine grobe Schätzung (Fußweg > 5 km): Knopf „trotzdem genau berechnen“
  DateTime? _zeit; // geplante Abfahrt bzw. Ankunft; null = jetzt
  bool _ankunft = false;
  int _gewaehlt = 0; // Verbindung, die die Karte zeigt
  List<Strecke> _strecken = [];
  Map<String, Halt> _halteDerVerbindung = {};
  List<LatLng> _naheHalte = [];
  StreamSubscription<Fix>? _gps; // nur solange eine Route sichtbar ist, die man gerade abläuft oder abfährt (D-36)
  Abweichung? _abweichung;
  DateTime? _wegBis; // danach sitzt man in der ersten Fahrt, deren Linie nur von Halt zu Halt gerade ist
  DateTime? _neuBerechnet;
  TempoProfil _tempo = TempoProfil('entspannt', 'fuss', standardKmh); // bis tempo.json gelesen ist

  @override
  void initState() {
    super.initState();
    Favoriten.instanz.laden();
    Losgehen.instanz.laden();
    WidgetsBinding.instance.addObserver(this);
    _starten();
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    final r = ModalRoute.of(context);
    if (r is PageRoute) seitenBeobachter.subscribe(this, r);
  }

  @override
  void didPushNext() {
    _verdeckt = true;
    _timer?.cancel();
    _wegStopp();
  }

  @override
  void didPopNext() {
    _verdeckt = false;
    _aktualisieren();
    _wegBeobachten();
  }

  @override
  void dispose() {
    seitenBeobachter.unsubscribe(this);
    WidgetsBinding.instance.removeObserver(this);
    _timer?.cancel();
    _wegStopp();
    _api?.schliessen();
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState s) {
    if (s == AppLifecycleState.resumed) {
      if (!_verdeckt) {
        _aktualisieren(standortNeu: true);
        _wegBeobachten();
      }
      _tempoNeuLesen(); // die Schnelleinstellung „Verkehrsmittel“ kann es geändert haben
      if (_api != null) KartenLager.instanz.start(_api!); // prüft höchstens täglich
    } else if (s == AppLifecycleState.paused || s == AppLifecycleState.hidden) {
      _timer?.cancel();
      _wegStopp();
    }
  }

  Future<void> _tempoNeuLesen() async {
    final t = await Tempo.laden();
    if (!mounted || t.gleich(_tempo)) return;
    setState(() => _tempo = t);
    if (_ziel != null) await _routeBerechnen();
  }

  Future<void> _starten() async {
    final (db, tempo, z) = await (HalteDb.oeffnen(), Tempo.laden(), Zugang.laden()).wait;
    _db = db;
    _tempo = tempo;
    if (z == null) return _zugangEinrichten();
    _setzeZugang(z);
  }

  void _setzeZugang(Zugang z) {
    _api?.schliessen();
    _zugang = z;
    _api = Api(z);
    // Mit gespeicherten Halten gleich Abfahrten zeigen, der Abgleich läuft nebenher; nur ein neuer Stand oder die
    // erste Liste (frisch installiert) löst danach noch eine Aktualisierung aus.
    final db = _db!;
    db.anzahl().then((n) {
      if (n > 0) _aktualisieren(standortNeu: true);
      _halteAbgleichen().then((neu) {
        if (neu || n == 0) _aktualisieren(standortNeu: true);
      });
    });
    KartenLager.instanz.start(_api!);
  }

  Future<void> _zugangEinrichten() async {
    final z = await Navigator.of(context).push<Zugang>(MaterialPageRoute(builder: (_) => Einstellungen(alt: _zugang)));
    if (z != null) _setzeZugang(z);
  }

  /// Haltestellenliste vom Server holen, wenn sie sich geändert hat (ETag = Fahrplanstand); true = neu geladen.
  Future<bool> _halteAbgleichen() async {
    final db = _db!, api = _api!;
    try {
      final neu = await api.halte(await db.etag());
      if (neu == null) return false;
      await db.ersetzen(neu.$1, neu.$2);
      return true;
    } on ApiFehler catch (e) {
      if (await db.anzahl() == 0 && mounted) setState(() => _hinweis = 'Haltestellen nicht geladen ($e)');
      return false;
    }
  }

  Future<void> _zielWaehlen() async {
    if (_api == null || _db == null) return;
    final o = await Navigator.of(context).push<Ort>(
        MaterialPageRoute(builder: (_) => ZielSuche(api: _api!, db: _db!, nahe: _pos)));
    if (o != null) await _zielSetzen(o);
  }

  Future<void> _zielSetzen(Ort o) async {
    setState(() {
      _ziel = o;
      _halt = null;
      _strecken = [];
    });
    await _routeBerechnen();
  }

  /// Abfahrten dieser Haltestelle (alle Bahnsteige gleichen Namens in der Nähe) unter der Karte zeigen.
  Future<void> _haltOeffnen(Halt h) async {
    final db = _db;
    if (db == null) return;
    final bahnsteige = bahnsteigeVon(h, [for (final (x, _) in await db.naechste(h.lat, h.lon, radius: 400)) x]);
    if (!mounted) return;
    setState(() => _halt = _GewaehlterHalt(halteOrt(h, bahnsteige), [for (final x in bahnsteige) x.id]));
    await _aktualisieren();
  }

  void _haltZu() {
    setState(() => _halt = null);
    _aktualisieren();
  }

  /// Karte angetippt: auf einer Haltestelle ihre Abfahrten, sonst der Punkt als Ziel, benannt nach der nächsten
  /// Haltestelle, weil es keine Rückwärtssuche gibt.
  Future<void> _karteAngetippt(LatLng p, double trefferMeter) async {
    if (_api == null || _db == null) return;
    if (trefferMeter > 0) {
      final treffer = await _db!.naechste(p.latitude, p.longitude, radius: trefferMeter);
      if (treffer.isNotEmpty) return _haltOeffnen(treffer.first.$1);
    }
    final nah = await _db!.naechste(p.latitude, p.longitude, radius: 300);
    final name = nah.isEmpty ? 'Punkt auf der Karte' : 'Punkt bei ${haltName(nah.first.$1.name)}';
    if (!mounted) return;
    setState(() {
      _ziel = Ort('karte', name, '', p.latitude, p.longitude);
      _halt = null;
      _strecken = [];
    });
    await _routeBerechnen();
  }

  void _zielWeg() => setState(() {
        _wegStopp();
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
        strecken.add(Strecke([von, ...form, nach], !a.auto, a.auto ? 'Auto' : ''));
      }
    }
    if (!mounted || _ziel != ziel) return;
    setState(() {
      _gewaehlt = i;
      _strecken = strecken;
      _halteDerVerbindung = halte;
    });
    _wegBeobachten();
  }

  void _wegStopp() {
    _gps?.cancel();
    _gps = null;
    _abweichung = null;
  }

  /// GPS mitlaufen lassen, solange die gewählte Verbindung gerade läuft: ohne Fahrt bis zum Ziel, mit Fahrt nur auf dem
  /// Fußweg zum ersten Halt. Geplante Fahrten (andere Zeit gewählt) und verdeckte Seiten brauchen das nicht.
  void _wegBeobachten() {
    _wegStopp();
    final v = _verbindungen;
    if (v == null || _gewaehlt >= v.length || _zeit != null || _verdeckt || _strecken.isEmpty) return;
    final erste = v[_gewaehlt].abschnitte.where((a) => a.fahrt).firstOrNull;
    if (erste != null && !_strecken.first.fuss) return; // beginnt direkt mit der Fahrt
    final weg = erste == null ? [for (final s in _strecken) ...s.punkte] : _strecken.first.punkte;
    if (erste != null && !DateTime.now().isBefore(erste.ab)) return;
    _wegBis = erste?.ab;
    _abweichung = Abweichung(weg, _tempo.art == 'auto' ? schwelleAuto : schwelleSonst);
    _gps = Standort.laufend().listen(_fix, onError: (_) => _wegStopp()); // keine Erlaubnis oder GPS aus: dann eben nicht
  }

  void _fix(Fix f) {
    final a = _abweichung, ziel = _ziel;
    if (a == null || ziel == null || !mounted) return;
    final bis = _wegBis;
    if ((bis != null && DateTime.now().isAfter(bis)) || abstandZumWeg(f.lat, f.lon, [a.weg.last]) < 30) return _wegStopp();
    final p = _pos;
    if (p == null || entfernung(p.lat, p.lon, f.lat, f.lon) > 10) setState(() => _pos = f); // Punkt auf der Karte läuft mit
    if (!a.pruefen(f.lat, f.lon, f.genau)) return;
    final zuletzt = _neuBerechnet;
    if (zuletzt != null && DateTime.now().difference(zuletzt) < _neuAbstand) return;
    _neuBerechnet = DateTime.now();
    _routeBerechnen(von: f);
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

  /// [von]: abgewichen, ab hier neu berechnen. Die alte Route bleibt stehen, bis die neue da ist, und bleibt bei einem Fehler.
  Future<void> _routeBerechnen({bool lang = false, Position? von}) async {
    final ziel = _ziel;
    if (ziel == null || _api == null) return;
    if (von == null) {
      _wegStopp();
      setState(() {
        _verbindungen = null;
        _routeHinweis = null;
        _langFrage = false;
      });
    }
    final p = von ?? await Standort.holen() ?? _pos;
    if (p == null) return setState(() => _routeHinweis = 'Kein Standort (Berechtigung oder GPS aus?)');
    _pos = p;
    try {
      final (roh, rt) = await _api!.route(p.lat, p.lon, ziel.lat, ziel.lon, kmh: _tempo.kmh, art: _tempo.serverArt, meiden: _tempo.serverMeiden, ticket: _tempo.ticket, wegArt: _tempo.wegArt, fern: _tempo.fern, lang: lang, ab: _zeit, ankunft: _ankunft, knapp: true);
      // früheste Ankunft oben; bei Gleichstand bleibt die Server-Reihenfolge
      final v = [for (final (i, x) in roh.indexed) (i, x)]
        ..sort((a, b) => a.$2.ankunft.compareTo(b.$2.ankunft) != 0 ? a.$2.ankunft.compareTo(b.$2.ankunft) : a.$1 - b.$1);
      if (!mounted || _ziel != ziel || (von != null && v.isEmpty)) return;
      if (von != null) ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('Route neu berechnet')));
      setState(() {
        _verbindungen = [for (final (_, x) in v) x];
        _routeRt = rt;
        if (v.isEmpty) _routeHinweis = 'Keine Verbindung gefunden';
      });
      // Karte zeigt zuerst die schnellste normal erreichbare Verbindung, nicht die knappe (D-31)
      final normal = _verbindungen!.indexWhere((x) => x.eilKmh == null);
      await _waehlen(normal < 0 ? 0 : normal);
    } on ApiFehler catch (e) {
      if (!mounted || _ziel != ziel || von != null) return;
      setState(() {
        _verbindungen = [];
        _langFrage = e.art == 'fussweg_lang';
        _routeHinweis = switch (e.art) {
          'fussweg_lang' => _langText(e.daten, zuFuss: _tempo.art == 'fuss'),
          'keine_haltestelle_in_reichweite' => switch (e.seite) {
              'start' => 'Start liegt außerhalb des Fahrplangebiets (bisher NRW)',
              'ziel' => 'Ziel liegt außerhalb des Fahrplangebiets (bisher NRW)',
              _ => 'Start und Ziel liegen außerhalb des Fahrplangebiets (bisher NRW)',
            },
          'anderer_betriebstag' => 'Fahrplan für diesen Tag noch nicht geladen',
          'fahrplan_laedt' => 'Server lädt gerade den Fahrplan',
          'kein_autoweg' => 'Keine Autoroute gefunden (Straßendaten bisher nur Raum Aachen, Köln, Bonn, Lüttich)',
          'auto_aus' => 'Autorouten gerade nicht verfügbar',
          'kein_weg' => 'Keinen direkten Weg gefunden (Wegedaten bisher nur Raum Aachen, Köln, Bonn, Lüttich)',
          'weg_aus' => 'Direkte Wege gerade nicht verfügbar',
          _ => 'Route nicht berechnet ($e)',
        };
      });
    }
  }

  static String _uhr(DateTime t) {
    final heute = DateUtils.isSameDay(t, DateTime.now());
    final h = '${t.hour.toString().padLeft(2, '0')}:${t.minute.toString().padLeft(2, '0')}';
    return heute ? h : '${t.day}.${t.month}. $h';
  }

  /// Abfahrt oder Ankunft planen (Fahrplan gilt bisher nur für den heutigen Betriebstag).
  Future<void> _zeitWaehlen() async {
    var ankunft = _ankunft;
    final ok = await showModalBottomSheet<bool>(
      context: context,
      builder: (c) => StatefulBuilder(
        builder: (c, setz) => SafeArea(
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Column(mainAxisSize: MainAxisSize.min, children: [
              SegmentedButton<bool>(
                segments: const [
                  ButtonSegment(value: false, label: Text('Abfahrt'), icon: Icon(Icons.logout)),
                  ButtonSegment(value: true, label: Text('Ankunft'), icon: Icon(Icons.flag_outlined)),
                ],
                selected: {ankunft},
                onSelectionChanged: (v) => setz(() => ankunft = v.first),
              ),
              const SizedBox(height: 16),
              FilledButton.icon(
                icon: const Icon(Icons.schedule),
                label: const Text('Uhrzeit wählen'),
                onPressed: () => Navigator.of(c).pop(true),
              ),
            ]),
          ),
        ),
      ),
    );
    if (ok != true || !mounted) return;
    final jetzt = DateTime.now(), alt = _zeit ?? jetzt;
    final u = await showTimePicker(
      context: context,
      initialTime: TimeOfDay.fromDateTime(alt),
      helpText: ankunft ? 'Ankommen bis' : 'Losgehen ab',
    );
    if (u == null || !mounted) return;
    var t = DateTime(jetzt.year, jetzt.month, jetzt.day, u.hour, u.minute);
    if (!ankunft && t.isBefore(jetzt.subtract(const Duration(minutes: 1)))) t = jetzt; // Vergangenheit = jetzt
    setState(() {
      _zeit = t;
      _ankunft = ankunft;
    });
    await _routeBerechnen();
  }

  /// Grobe Schätzung des Servers (Luftlinie × 1,3) als Text, z. B. „Vom Start bis zum nächsten Halt ca. 6,5 km zu Fuß (~1 h 30 min).“
  static String _langText(Map? d, {required bool zuFuss}) {
    final wie = zuFuss ? 'zu Fuß' : 'selbst unterwegs';
    String teil(String wer, Map? s) {
      if (s == null) return '';
      final km = (s['km'] as num).toDouble();
      final min = (s['min'] as num).toInt();
      final dauer = min >= 60 ? '${min ~/ 60} h ${min % 60} min' : '$min min';
      return '$wer ca. ${km.toStringAsFixed(km % 1 == 0 ? 0 : 1).replaceAll('.', ',')} km $wie (~$dauer).\n';
    }

    return '${teil('Vom Start bis zum nächsten Halt', d?['start'] as Map?)}'
        '${teil('Vom letzten Halt bis zum Ziel', d?['ziel'] as Map?)}'
        'Das dauert $wie lange (grobe Schätzung).';
  }

  Future<void> _aktualisieren({bool standortNeu = false}) async {
    _timer?.cancel();
    if (_api == null || !mounted) return;
    if (standortNeu || _pos == null) {
      final p = await Standort.holen();
      if (p == null && _pos == null) {
        setState(() => _hinweis = 'Kein Standort (Berechtigung oder GPS aus?)');
        if (_halt == null) return; // eine angetippte Haltestelle braucht keinen Standort
      } else {
        _pos = p ?? _pos;
        _gruppen = await _gruppieren(_pos!);
        Losgehen.instanz.pruefen(_api!, _pos!, _tempo).ignore(); // nebenher, höchstens alle 5 min
      }
    }
    final ids = [for (final g in _gruppen) ...g.ids].take(_maxIds).toList();
    if (_halt case final halt?) {
      // Nur die gewählte Haltestelle abfragen; die Liste darunter ist nicht zu sehen.
      try {
        final (abf, rt) = await _api!.abfahrten(halt.ids, n: 10);
        halt.abfahrten = [for (final id in halt.ids) ...?abf[id]]..sort((a, b) => a.erwartet.compareTo(b.erwartet));
        halt.rt = rt;
        halt.fehler = null;
      } on ApiFehler catch (e) {
        halt.fehler = 'Abfahrten nicht geladen ($e)';
      }
    } else if (ids.isNotEmpty) {
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
    _timer?.cancel(); // überlappende Aufrufe (Start, Abgleich, Rückkehr) sollen nur einen Takt hinterlassen
    if (!_verdeckt) _timer = Timer(_takt, _aktualisieren);
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
        gruppen[k] = _Gruppe(haltName(h.name), m, [h.id], h);
      }
    }
    return gruppen.values.toList();
  }

  @override
  Widget build(BuildContext context) {
    final farbe = Theme.of(context).colorScheme;
    final halt = _halt;
    return PopScope(
      canPop: halt == null, // Zurück schließt zuerst die Haltestelle
      onPopInvokedWithResult: (geschlossen, _) {
        if (!geschlossen && _halt != null) _haltZu();
      },
      child: Scaffold(
      appBar: AppBar(
        title: const Text('SchnellWeg'),
        actions: [
          IconButton(
            icon: const Icon(Icons.settings),
            tooltip: 'Einstellungen',
            onPressed: () => Navigator.of(context).push(MaterialPageRoute(
              builder: (_) => EinstellungenSeite(
                tempo: _tempo,
                zugang: _zugang,
                tempoGeaendert: (p) {
                  setState(() => _tempo = p);
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
            if (_ziel != null)
              Align(
                alignment: Alignment.centerLeft,
                child: Padding(
                  padding: const EdgeInsets.only(top: 6),
                  child: Wrap(spacing: 6, children: [
                    InputChip(
                      avatar: const Icon(Icons.schedule, size: 18),
                      label: Text(_zeit == null ? 'Jetzt losgehen' : '${_ankunft ? 'Ankunft' : 'Abfahrt'} ${_uhr(_zeit!)}'),
                      onPressed: _zeitWaehlen,
                      onDeleted: _zeit == null
                          ? null
                          : () {
                              setState(() => _zeit = null);
                              _routeBerechnen();
                            },
                    ),
                    ActionChip(
                      avatar: Icon(_tempo.icon, size: 18),
                      label: Text(_tempo.anzeige),
                      tooltip: 'Verkehrsmittel',
                      onPressed: () async {
                        final p = await verkehrsmittelWaehlen(context);
                        if (p == null || !mounted) return;
                        setState(() => _tempo = p);
                        _routeBerechnen();
                      },
                    ),
                  ]),
                ),
              ),
            if (_ziel == null)
              ListenableBuilder(
                listenable: Favoriten.instanz,
                builder: (context, _) => Favoriten.instanz.eintraege.isEmpty
                    ? const SizedBox.shrink()
                    : SizedBox(
                        height: 44,
                        child: ListView(scrollDirection: Axis.horizontal, padding: const EdgeInsets.only(top: 6), children: [
                          for (final f in Favoriten.instanz.oben)
                            Padding(
                              padding: const EdgeInsets.only(right: 6),
                              child: GestureDetector(
                                onLongPress: () => favoritBearbeiten(context, f),
                                child: ActionChip(
                                  avatar: Icon(Icons.star, size: 18, color: Colors.amber.shade700),
                                  label: Text(f.name),
                                  onPressed: () => _zielSetzen(f),
                                ),
                              ),
                            ),
                          for (final (name, orte) in Favoriten.instanz.ordner)
                            Padding(
                              padding: const EdgeInsets.only(right: 6),
                              child: GestureDetector(
                                onLongPress: () => favoritenOrdnen(context),
                                child: ActionChip(
                                  avatar: const Icon(Icons.folder_outlined, size: 18),
                                  label: Text('$name (${orte.length})'),
                                  onPressed: () async {
                                    final o = await ausOrdnerWaehlen(context, name, orte);
                                    if (o != null && mounted) _zielSetzen(o);
                                  },
                                ),
                              ),
                            ),
                        ]),
                      ),
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
            markiert: halt == null ? null : LatLng(halt.ort.lat, halt.ort.lon),
            onAntippen: _karteAngetippt,
          ),
        ),
        if (halt != null) ...[
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 4, 4, 0),
            child: Row(children: [
              Expanded(
                child: Text(halt.ort.name,
                    maxLines: 1, overflow: TextOverflow.ellipsis, style: Theme.of(context).textTheme.titleSmall),
              ),
              if (halt.abfahrten?.isNotEmpty == true && !halt.rt)
                Text('nur Fahrplan', style: TextStyle(fontSize: 12, color: farbe.outline)),
              FavoritStern(halt.ort),
              TextButton.icon(
                onPressed: () => _zielSetzen(halt.ort),
                icon: const Icon(Icons.directions, size: 18),
                label: const Text('Hierhin'),
              ),
              IconButton(icon: const Icon(Icons.close), tooltip: 'Schließen', onPressed: _haltZu),
            ]),
          ),
          Expanded(
            flex: 6,
            child: HaltAbfahrten(abfahrten: halt.abfahrten, fehler: halt.fehler, onRefresh: _aktualisieren),
          ),
        ] else ...[
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 12, 16, 4),
            child: Row(children: [
              Text(_ziel == null ? 'Haltestellen in der Nähe' : 'Verbindungen',
                  style: Theme.of(context).textTheme.titleSmall),
              const Spacer(),
              if (_ziel != null) FavoritStern(_ziel!, klein: true),
              if (_ziel == null ? _gruppen.isNotEmpty && !_rt : (_verbindungen?.isNotEmpty ?? false) && !_routeRt)
                Text('nur Fahrplan', style: TextStyle(fontSize: 12, color: farbe.outline)),
            ]),
          ),
          Expanded(flex: 6, child: _ziel == null ? _halteListe() : _routeListe()),
        ],
      ]),
      ),
    );
  }

  Widget _halteListe() => _hinweis != null && _gruppen.every((g) => g.abfahrten.isEmpty)
      ? Center(child: Text(_hinweis!))
      : RefreshIndicator(
          onRefresh: () => _aktualisieren(standortNeu: true),
          child: ListenableBuilder(
            listenable: Losgehen.instanz,
            builder: (context, _) => ListView(children: [
              if (Losgehen.instanz.plan case final p?) _TerminZeile(p, onTap: () => _terminAlsZiel(p)),
              for (final g in _gruppen) _HaltZeile(g, onTap: () => _haltOeffnen(g.naechster)),
            ]),
          ),
        );

  /// Nächster Termin des Losgeh-Weckers als Ziel, mit Ankunft bis Beginn − Puffer.
  void _terminAlsZiel(Losgehplan p) {
    final ziel = p.ziel;
    if (ziel == null) return;
    _zeit = p.termin.beginn.subtract(Duration(minutes: Losgehen.instanz.puffer));
    _ankunft = true;
    _zielSetzen(Ort(ziel.art, p.termin.titel, ziel.name, ziel.lat, ziel.lon));
  }

  Widget _routeListe() {
    final v = _verbindungen;
    if (v == null) return const Center(child: CircularProgressIndicator());
    if (v.isEmpty && _langFrage) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(mainAxisSize: MainAxisSize.min, children: [
            Text(_routeHinweis ?? '', textAlign: TextAlign.center),
            const SizedBox(height: 16),
            FilledButton(onPressed: () => _routeBerechnen(lang: true), child: const Text('Trotzdem genau berechnen')),
          ]),
        ),
      );
    }
    if (v.isEmpty) return Center(child: Text(_routeHinweis ?? 'Keine Verbindung gefunden'));
    return RefreshIndicator(
      onRefresh: _routeBerechnen,
      child: ListView(children: [
        for (final (i, x) in v.indexed) _VerbindungZeile(x, kmh: _tempo.kmh, zuFuss: _tempo.art == 'fuss', bezug: _ankunft ? null : _zeit ?? DateTime.now(), gewaehlt: i == _gewaehlt, onTap: () => _details(i)),
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
  final VoidCallback onTap;
  const _HaltZeile(this.g, {required this.onTap});

  @override
  Widget build(BuildContext context) {
    final farbe = Theme.of(context).colorScheme;
    final jetzt = DateTime.now();
    return InkWell(
      onTap: onTap,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 6),
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Row(children: [
            Expanded(child: Text(g.name, style: const TextStyle(fontWeight: FontWeight.w600))),
            Text('${(g.meter / 10).round() * 10} m', style: TextStyle(color: farbe.outline)),
          ]),
          for (final a in g.abfahrten.take(3))
            Padding(padding: const EdgeInsets.only(left: 12, top: 2), child: AbfahrtZeile(a, jetzt)),
        ]),
      ),
    );
  }
}

String _uhr(DateTime t) => '${t.hour.toString().padLeft(2, '0')}:${t.minute.toString().padLeft(2, '0')}';

/// Kurzfassung nach app-ui.md: 🚶 4 min → 33 Elisenbrunnen 12:41 (+1) → 🚶 2 min → an 13:02 · 1 Umstieg
class _VerbindungZeile extends StatelessWidget {
  final Verbindung v;
  final double kmh; // gewähltes Tempo, für den Hinweis bei knappen Verbindungen
  final bool zuFuss;
  final DateTime? bezug; // ab wann man losgehen kann; null im Ankunftsmodus (kein Gehhinweis)
  final bool gewaehlt;
  final VoidCallback onTap;
  const _VerbindungZeile(this.v, {required this.kmh, required this.zuFuss, this.bezug, required this.gewaehlt, required this.onTap});

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
        teile.add(TextSpan(text: '${a.auto ? '🚗' : const {'rad': '🚲', 'eigen': '➡️'}[a.wegArt] ?? '🚶'} $min min'));
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

    final kopf = [
      '${_uhr(v.ab)} – ${_uhr(v.ankunft)}',
      '${minuten(v.ankunft.difference(v.ab))} min',
      v.artText,
    ].join(' · ');
    String zahl(double x) => x.toStringAsFixed(x == x.roundToDouble() ? 0 : 1).replaceAll('.', ',');
    final eil = v.eilKmh;
    // Gehhinweis mit Puffer (D-33): nur zu Fuß, nur wenn die Verbindung mit einem Fußweg zum ersten Halt beginnt
    (Color, String)? gehen;
    if (eil == null && zuFuss && bezug != null && v.abschnitte.length >= 2 && !v.abschnitte[0].fahrt && v.abschnitte[1].fahrt) {
      final (w, f) = (v.abschnitte[0], v.abschnitte[1]);
      final weg = w.an.difference(w.ab), frei = (f.abRt ?? f.ab).difference(bezug!);
      final luft = (frei - weg).inMinutes;
      final luftText = luft > 0 ? ' · $luft min Luft' : '';
      gehen = switch (gehHinweis(weg, frei, kmh)) {
        (GehStufe.entspannter, final n) => (farbe.primary, '🌿 Entspannt: ${n < 2 ? 'viel Zeit' : '${zahl(n)} km/h reichen'}$luftText'),
        (GehStufe.normal, _) => (farbe.outline, '🚶 Normales Tempo passt$luftText'),
        (GehStufe.schneller, final n) => (farbe.tertiary, '⚡ Schneller gehen: ${zahl(n)} km/h (sonst ${zahl(kmh)})'),
        null => null,
      };
    }
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
          if (eil != null)
            Padding(
              padding: const EdgeInsets.only(top: 4),
              child: Text('⚡ Knapp: nur mit ${zahl(eil)} km/h zum ersten Halt (sonst ${zahl(kmh)}) – sofort los',
                  style: TextStyle(color: farbe.tertiary, fontWeight: FontWeight.w600)),
            ),
          if (gehen != null)
            Padding(
              padding: const EdgeInsets.only(top: 4),
              child: Text(gehen.$2, style: TextStyle(color: gehen.$1)),
            ),
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
