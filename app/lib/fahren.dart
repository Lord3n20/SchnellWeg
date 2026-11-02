// Fahransicht fürs Auto (D-40), angelehnt an Google Maps und Waze: Karte in Fahrtrichtung, nächster Abbiegepfeil oben, Ankunft unten,
// Stau rot auf der Route. Keine Sprachansage (Betreiber). Eine Umfahrung bleibt ein Angebot, das man antippen muss (D-39).
import 'dart:async';

import 'package:flutter/material.dart';
import 'package:latlong2/latlong.dart';

import 'abweichung.dart';
import 'api.dart';
import 'halte_db.dart';
import 'karte.dart';
import 'standort.dart';

const _aufDerRouteM = 35.0; // so nah am Weg zeigt die Ansicht den Punkt auf dem Weg statt das zappelnde GPS
const _angekommenM = 40.0;
const _neuAbstand = Duration(seconds: 15);
const _auffrischen = Duration(minutes: 3); // Stau und Ankunft; der Server fragt dafür höchstens seinen Stand ab
const _gruen = Color(0xFF0B6E4F);

/// Pfeil zu Valhallas Manövertyp.
IconData schrittIcon(int typ) => switch (typ) {
      4 || 5 || 6 => Icons.place,
      9 || 23 => Icons.turn_slight_right,
      10 || 2 => Icons.turn_right,
      11 => Icons.turn_sharp_right,
      12 => Icons.u_turn_right,
      13 => Icons.u_turn_left,
      14 => Icons.turn_sharp_left,
      15 || 3 => Icons.turn_left,
      16 || 24 => Icons.turn_slight_left,
      18 || 20 => Icons.ramp_right,
      19 || 21 => Icons.ramp_left,
      25 || 37 || 38 => Icons.merge,
      26 || 27 => Icons.roundabout_right,
      28 || 29 => Icons.directions_boat,
      _ => Icons.straight,
    };

class Fahren extends StatefulWidget {
  final Verbindung v;
  final Verbindung? angebot; // Umfahrung, die man antippen kann
  final LatLng start, ziel;
  final Future<List<Verbindung>> Function(Position von) neu; // Route ab hier, normale Route zuerst
  const Fahren({super.key, required this.v, this.angebot, required this.start, required this.ziel, required this.neu});

  @override
  State<Fahren> createState() => _FahrenState();
}

class _FahrenState extends State<Fahren> with SingleTickerProviderStateMixin, WidgetsBindingObserver {
  late Verbindung _v = widget.v;
  late Verbindung? _angebot = widget.angebot;
  late bool _aufUmfahrung = widget.v.umfahrung.isNotEmpty;
  List<LatLng> _form = [];
  List<Schritt> _schritte = [];
  double _gesamtM = 0, _gesamtSec = 0, _restM = 0, _kmh = -1;
  (Schritt, double)? _naechster;
  Abweichung? _abweichung;
  StreamSubscription<Fix>? _gps;
  Timer? _takt;
  DateTime? _neuBerechnet;
  bool _angekommen = false, _ohneGps = false;

  final _blick = ValueNotifier<Fahrblick?>(null);
  late final _laeufer = AnimationController(vsync: this, duration: const Duration(milliseconds: 1000))..addListener(_schritt);
  late Fahrblick _von = Fahrblick(widget.start, 0, 17), _nach = _von;
  DateTime _zuletzt = DateTime(0);
  LatLng? _letzterFix;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _route(widget.v);
    if (_form.length >= 2) {
      _von = _nach = Fahrblick(widget.start, kurs(_form[0], _form[1]), 17);
      _blick.value = _von;
    }
    _gpsAn();
    _takt = Timer.periodic(_auffrischen, (_) => _frisch());
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _takt?.cancel();
    _gps?.cancel();
    _laeufer.dispose();
    _blick.dispose();
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState s) {
    if (s == AppLifecycleState.resumed) {
      if (!_angekommen) _gpsAn();
    } else if (s == AppLifecycleState.paused || s == AppLifecycleState.hidden) {
      _gps?.cancel();
      _gps = null;
    }
  }

  void _gpsAn() {
    _gps?.cancel();
    _gps = Standort.laufend(bildschirmAn: true).listen(_fix, onError: (_) {
      if (mounted) setState(() => _ohneGps = true);
    });
  }

  void _route(Verbindung v) {
    final a = v.abschnitte.firstWhere((a) => a.auto, orElse: () => v.abschnitte.first);
    _v = v;
    _form = [for (final (lat, lon) in polyline(a.form)) LatLng(lat, lon)];
    _schritte = a.schritte;
    _gesamtSec = v.ankunft.difference(v.ab).inSeconds.toDouble();
    _gesamtM = _form.length < 2 ? 0 : restMeter(_form, (i: 0, punkt: _form.first, m: 0));
    _restM = _gesamtM;
    _naechster = naechsterSchritt(_form, _schritte, _von.pos.latitude, _von.pos.longitude);
    _abweichung = _form.length < 2 ? null : Abweichung(_form, schwelleAuto);
  }

  void _schritt() {
    final jetzt = DateTime.now();
    if (jetzt.difference(_zuletzt).inMilliseconds < 33) return; // 30 Bilder je Sekunde reichen
    _zuletzt = jetzt;
    final t = _laeufer.value;
    _blick.value = Fahrblick(
      LatLng(_von.pos.latitude + (_nach.pos.latitude - _von.pos.latitude) * t,
          _von.pos.longitude + (_nach.pos.longitude - _von.pos.longitude) * t),
      kursMisch(_von.kurs, _nach.kurs, t),
      _von.zoom + (_nach.zoom - _von.zoom) * t,
    );
  }

  void _fix(Fix f) {
    if (!mounted || _form.length < 2) return;
    final fixPunkt = LatLng(f.lat, f.lon);
    final hier = aufWeg(_form, f.lat, f.lon);
    final kmh = f.tempo < 0 ? _kmh : f.tempo * 3.6;
    var ziel = fixPunkt;
    var k = _nach.kurs;
    if (hier.m <= _aufDerRouteM) {
      ziel = hier.punkt;
      if (hier.i + 1 < _form.length) k = kurs(_form[hier.i], _form[hier.i + 1]);
    } else {
      final l = _letzterFix;
      if (l != null && entfernung(l.latitude, l.longitude, f.lat, f.lon) > 5) k = kurs(l, fixPunkt);
    }
    _letzterFix = fixPunkt;
    _von = _blick.value ?? _von;
    _nach = Fahrblick(ziel, k, kmh < 0 ? _nach.zoom : fahrZoom(kmh));
    _laeufer.forward(from: 0);
    final rest = restMeter(_form, hier);
    setState(() {
      _ohneGps = false;
      _kmh = kmh;
      _restM = rest;
      _naechster = naechsterSchritt(_form, _schritte, f.lat, f.lon);
      if (rest < _angekommenM) _angekommen = true;
    });
    if (_angekommen) {
      _gps?.cancel();
      _gps = null;
      _takt?.cancel();
      return;
    }
    if (_abweichung?.pruefen(f.lat, f.lon, f.genau) != true) return;
    final z = _neuBerechnet;
    if (z != null && DateTime.now().difference(z) < _neuAbstand) return;
    _neuBerechnet = DateTime.now();
    _laden(f, abgewichen: true);
  }

  /// Stau und Ankunft auffrischen. Eine gewählte Umfahrung bleibt, sonst käme man wieder auf die normale Route.
  void _frisch() {
    final f = _nach.pos;
    if (_aufUmfahrung || _angekommen) return;
    _laden(Position(f.latitude, f.longitude, 0));
  }

  Future<void> _laden(Position von, {bool abgewichen = false}) async {
    try {
      final l = await widget.neu(von);
      if (!mounted || l.isEmpty) return;
      setState(() {
        _route(l.first);
        _aufUmfahrung = l.first.umfahrung.isNotEmpty;
        _angebot = l.where((x) => x.umfahrung.isNotEmpty && x != l.first).firstOrNull;
      });
      if (abgewichen) ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('Route neu berechnet')));
    } on ApiFehler {
      // alte Route bleibt, beim nächsten Mal wieder
    }
  }

  void _umfahrungNehmen() {
    final a = _angebot;
    if (a == null) return;
    setState(() {
      _route(a);
      _angebot = null;
      _aufUmfahrung = true;
    });
  }

  @override
  Widget build(BuildContext context) {
    final farbe = Theme.of(context).colorScheme;
    final restSec = _gesamtM <= 0 ? 0.0 : _gesamtSec * _restM / _gesamtM;
    final an = DateTime.now().add(Duration(seconds: restSec.round()));
    final restMin = (restSec / 60).round();
    final uhr = '${an.hour.toString().padLeft(2, '0')}:${an.minute.toString().padLeft(2, '0')}';
    final km = _restM < 1000 ? '${(_restM / 10).round() * 10} m' : '${(_restM / 1000).toStringAsFixed(_restM < 10000 ? 1 : 0).replaceAll('.', ',')} km';
    final dauer = restMin < 60 ? '$restMin min' : '${restMin ~/ 60} h ${restMin % 60} min';
    final angebot = _angebot;
    final gewinn = angebot == null ? 0 : ((_gesamtSec * _restM / (_gesamtM <= 0 ? 1 : _gesamtM) - angebot.ankunft.difference(angebot.ab).inSeconds) / 60).round();
    return Scaffold(
      body: Column(children: [
        Container(
          color: _gruen,
          child: SafeArea(
            bottom: false,
            child: Padding(
              padding: const EdgeInsets.fromLTRB(16, 12, 16, 14),
              child: _Banner(naechster: _naechster, angekommen: _angekommen, ohneGps: _ohneGps),
            ),
          ),
        ),
        Expanded(
          child: Stack(children: [
            Positioned.fill(
              child: Karte(
                standort: widget.start,
                ziel: widget.ziel,
                strecken: [if (_form.length >= 2) Strecke(_form, false, 'Auto')],
                meldungen: _v.verkehr,
                fahrblick: _blick,
              ),
            ),
            if (_kmh >= 0)
              Positioned(
                left: 12,
                bottom: 32,
                child: CircleAvatar(
                  radius: 30,
                  backgroundColor: farbe.surface,
                  child: Column(mainAxisSize: MainAxisSize.min, children: [
                    Text('${_kmh.round()}', style: TextStyle(fontSize: 20, fontWeight: FontWeight.w700, color: farbe.onSurface)),
                    Text('km/h', style: TextStyle(fontSize: 10, color: farbe.onSurfaceVariant)),
                  ]),
                ),
              ),
            if (angebot != null && gewinn >= 1)
              Positioned(
                right: 12,
                top: 12,
                child: FilledButton.tonalIcon(
                  onPressed: _umfahrungNehmen,
                  icon: const Icon(Icons.alt_route),
                  label: Text('Umfahrung spart $gewinn min'),
                ),
              ),
          ]),
        ),
        Material(
          color: farbe.surface,
          elevation: 8,
          child: SafeArea(
            top: false,
            child: Padding(
              padding: const EdgeInsets.fromLTRB(20, 10, 8, 10),
              child: Row(children: [
                Expanded(
                  child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                    Text(dauer, style: const TextStyle(fontSize: 24, fontWeight: FontWeight.w700, color: _gruen)),
                    Text('$km · Ankunft $uhr', style: TextStyle(color: farbe.onSurfaceVariant)),
                  ]),
                ),
                IconButton.filledTonal(
                  tooltip: 'Fahrt beenden',
                  icon: const Icon(Icons.close),
                  onPressed: () => Navigator.of(context).pop(),
                ),
              ]),
            ),
          ),
        ),
      ]),
    );
  }
}

class _Banner extends StatelessWidget {
  final (Schritt, double)? naechster;
  final bool angekommen, ohneGps;
  const _Banner({required this.naechster, required this.angekommen, required this.ohneGps});

  @override
  Widget build(BuildContext context) {
    const weiss = TextStyle(color: Colors.white);
    final n = naechster;
    final (IconData icon, String gross, String klein) = angekommen
        ? (Icons.place, 'Angekommen', '')
        : ohneGps
            ? (Icons.gps_off, 'Kein GPS', 'Standort-Erlaubnis oder GPS prüfen')
            : n == null
                ? (Icons.straight, 'Der Route folgen', '')
                : (schrittIcon(n.$1.typ), inMetern(n.$2).replaceFirst('In ', ''), n.$1.text);
    return Row(children: [
      Icon(icon, color: Colors.white, size: 52),
      const SizedBox(width: 14),
      Expanded(
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Text(gross, style: weiss.copyWith(fontSize: 28, fontWeight: FontWeight.w700)),
          if (klein.isNotEmpty) Text(klein, maxLines: 2, overflow: TextOverflow.ellipsis, style: weiss.copyWith(fontSize: 16)),
        ]),
      ),
    ]);
  }
}
