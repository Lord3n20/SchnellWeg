// Tempo-Profile verwalten und per Messgang kalibrieren: ein paar Minuten im Profiltempo gehen oder fahren, die App
// mittelt das GPS-Tempo über die Zeit in Bewegung (Stehen an Ampeln zählt nicht). Die Strecke zeichnet ein Hintergrunddienst auf (D-45);
// am Ende geht sie einmal zum eigenen Server, der Steigung und Belag zuordnet und nichts davon aufhebt (D-44).
import 'dart:async';
import 'dart:math';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'hang_messung.dart';
import 'messung.dart';
import 'standort.dart';
import 'tempo.dart';

const _genauMax = 20.0; // m; ungenauere Positionen zählen nicht
/// Art → (min, max) m/s in Bewegung; darunter Stehen, darüber etwas anderes (Rennen, Bus, Auto).
const _bewegung = {
  'fuss': (0.6, 3.0),
  'eigen': (1.0, 14.0),
};
const _spurMax = 15000;
const _lueckeMax = 5000.0; // ms; größere Lücken im GPS werden nicht überbrückt
const _zeitMin = Duration(minutes: 2), _streckeMin = 150.0;

/// Unterpunkt „Profile“: auswählen, anlegen, bearbeiten, messen, löschen.
class TempoSeite extends StatefulWidget {
  const TempoSeite({super.key});

  @override
  State<TempoSeite> createState() => _TempoSeiteState();
}

class _TempoSeiteState extends State<TempoSeite> {
  Tempo? _t;
  String? _offen; // Profil mit angefangener Messung

  @override
  void initState() {
    super.initState();
    Tempo.oeffnen().then((t) {
      if (mounted) setState(() => _t = t);
    });
    _offenLaden();
  }

  Future<void> _offenLaden() async {
    final o = await Messung.offen();
    if (mounted) setState(() => _offen = o);
  }

  Future<void> _aendern(void Function(Tempo) f) async {
    setState(() => f(_t!));
    await _t!.speichern();
  }

  Future<void> _messen(TempoProfil gewaehlt) async {
    // Eine angefangene Messung geht vor; gehört sie zu einem gelöschten Profil, ist sie nichts mehr wert.
    final offen = await Messung.offen();
    final p = _t!.profile.where((x) => x.name == offen && x.messbar).firstOrNull ?? gewaehlt;
    if (offen != null && p.name != offen) await Messung.verwerfen();
    if (!mounted) return;
    final r = await Navigator.of(context).push<(double, HangErgebnis?)>(MaterialPageRoute(builder: (_) => MessSeite(profil: p, fuss: _t!.profile.where((x) => x.art == 'fuss').firstOrNull?.name)));
    if (r == null) return _offenLaden();
    final (kmh, e) = r;
    await _aendern((_) {
      p.kmh = e != null && p.erlaubt(e.kmhEben) ? e.kmhEben : kmh;
      p.gemessen = DateTime.now();
      if (e != null) {
        p.auf = e.auf ?? p.auf;
        p.ab = e.ab ?? p.ab;
        p.zuFussAb = e.zuFussAb ?? p.zuFussAb;
        e.belag.forEach((k, v) => v == null ? p.belagRegeln.remove(k) : p.belagRegeln[k] = v);
      }
    });
    await Messung.verwerfen(); // erst jetzt, das Ergebnis ist gespeichert
    await _offenLaden();
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(_messText(p, e))));
  }

  /// Bearbeiten oder (neu) anlegen; gespeichert wird erst beim Übernehmen.
  Future<void> _bearbeiten(TempoProfil p, {bool neu = false}) async {
    final x = await Navigator.of(context).push<TempoProfil>(MaterialPageRoute(builder: (_) => ProfilSeite(p.kopie(), neu: neu, andere: [for (final x in _t!.profile) if (x.name != p.name && x.messbar) (x.name, x.anzeige)])));
    if (x == null) return;
    await _aendern((t) {
      if (neu) {
        t.profile.add(x);
      } else {
        t.profile[t.profile.indexWhere((a) => a.name == x.name)] = x;
      }
    });
  }

  Future<void> _loeschen(TempoProfil p) async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text('„${p.anzeige}“ löschen?'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('Abbrechen')),
          FilledButton(onPressed: () => Navigator.pop(ctx, true), child: const Text('Löschen')),
        ],
      ),
    );
    if (ok != true) return;
    await _aendern((t) {
      t.profile.remove(p);
      if (t.aktiv == p.name) t.aktiv = 'entspannt';
    });
  }

  @override
  Widget build(BuildContext context) {
    final t = _t;
    return Scaffold(
      appBar: AppBar(title: const Text('Profile')),
      floatingActionButton: t == null
          ? null
          : FloatingActionButton.extended(
              icon: const Icon(Icons.add),
              label: const Text('Profil'),
              onPressed: () => _bearbeiten(TempoProfil('p${DateTime.now().millisecondsSinceEpoch}', 'eigen', 15), neu: true),
            ),
      body: t == null
          ? const Center(child: CircularProgressIndicator())
          : ListView(padding: const EdgeInsets.only(bottom: 88), children: [
              const Padding(
                padding: EdgeInsets.fromLTRB(16, 16, 16, 8),
                child: Text('Das ausgewählte Profil gilt für Wege zu Haltestellen, Umstiege und direkte Wege. '
                    'Name und Tempo bleiben auf dem Handy, der Server erfährt nur das Tempo und was er meiden soll.'),
              ),
              for (final p in t.profile) ListTile(
                leading: Icon(p.name == t.aktiv ? Icons.radio_button_checked : Icons.radio_button_unchecked,
                    color: p.name == t.aktiv ? Theme.of(context).colorScheme.primary : null),
                title: Text(p.anzeige),
                subtitle: Text(!p.messbar ? 'höchstens ${tempoText(p.kmh)}' : p.name == _offen ? 'Messung angefangen, unter „Messen“ abschließen' : '${tempoText(p.kmh)} · '
                    '${p.gemessen == null ? 'nicht gemessen' : 'gemessen am ${p.gemessen!.day}.${p.gemessen!.month}.'}'),
                onTap: () => _aendern((t) => t.aktiv = p.name),
                trailing: PopupMenuButton<String>(
                  onSelected: (w) => switch (w) { 'messen' => _messen(p), 'weg' => _loeschen(p), _ => _bearbeiten(p) },
                  itemBuilder: (_) => [
                    const PopupMenuItem(value: 'hand', child: ListTile(leading: Icon(Icons.tune), title: Text('Bearbeiten'))),
                    if (p.messbar) const PopupMenuItem(value: 'messen', child: ListTile(leading: Icon(Icons.gps_fixed), title: Text('Messen'))),
                    if (p.eigen) const PopupMenuItem(value: 'weg', child: ListTile(leading: Icon(Icons.delete_outline), title: Text('Löschen'))),
                  ],
                ),
              ),
            ]),
    );
  }
}

const _belagArten = [(1, 'Erde, Gras, Sand'), (2, 'Schotter'), (3, 'Grobes Pflaster'), (4, 'Kopfsteinpflaster'), (5, 'Gehwegplatten')];

String _messText(TempoProfil p, HangErgebnis? e) {
  if (e == null) return 'Tempo gespeichert. Hang und Belag ließen sich nicht auswerten, die Einstellungen bleiben.';
  if (e.auf == null && e.ab == null && e.zuFussAb == null && e.belag.isEmpty) return 'Tempo gespeichert. Für Hang und Belag war zu wenig dabei.';
  return 'Gemessen: eben ${tempoText(p.kmh)}, '
      '${p.zuFussAb > 0 ? 'zu Fuß ab ${p.zuFussAb} %' : 'bergauf ${p.auf} %'}, bergab ${p.ab > 0 ? '+' : ''}${p.ab} %'
      '${e.belag.isEmpty ? '' : ', ${e.belag.length} Beläge'}';
}

/// Stufen, was die Route meiden soll: Schlüssel wie in /v1/route, Text, für welche Profile.
const _stufen = [
  ('treppen', 'Treppen', {'fuss', 'eigen'}),
  ('hauptstrassen', 'Hauptstraßen', {'eigen'}),
  ('dunkel', 'Unbeleuchtete Wege', {'fuss', 'eigen'}),
  ('faehren', 'Fähren', {'fuss', 'eigen', 'auto'}),
  ('autobahn', 'Autobahn', {'auto'}),
  ('maut', 'Mautstraßen', {'auto'}),
];

/// Ein Profil bearbeiten: liefert die geänderte Kopie beim Übernehmen.
class ProfilSeite extends StatefulWidget {
  final TempoProfil p;
  final bool neu; // sonst steht der bisherige Name im Feld
  final List<(String, String)> andere; // Schlüssel und Anzeigename der übrigen Profile, für den Belag
  const ProfilSeite(this.p, {super.key, this.neu = false, this.andere = const []});

  @override
  State<ProfilSeite> createState() => _ProfilSeiteState();
}

class _ProfilSeiteState extends State<ProfilSeite> {
  late final TempoProfil p = widget.p;
  late final _name = TextEditingController(text: p.titel ?? (widget.neu ? '' : p.anzeige));
  late final _tempo = TextEditingController(text: p.kmh.toString().replaceAll('.', ','));

  @override
  void dispose() {
    _name.dispose();
    _tempo.dispose();
    super.dispose();
  }

  double? get _kmh {
    final v = double.tryParse(_tempo.text.replaceAll(',', '.'));
    return v != null && p.erlaubt(v) ? v : null;
  }

  bool get _ok => _kmh != null && (!p.eigen || _name.text.trim().isNotEmpty);

  void _uebernehmen() {
    if ((_kmh! - p.kmh).abs() >= 0.05) {
      p.kmh = _kmh!;
      p.gemessen = null;
    }
    if (p.eigen) p.titel = _name.text.trim();
    if (p.meiden['bus'] == true || p.meiden['oev'] == true) p.meiden.remove('bus_min'); // ausgeblendet, soll nicht mitgehen
    Navigator.pop(context, p);
  }

  Widget _belagZeile(int k, String text) {
    final wert = p.belagRegeln[k];
    final auswahl = <DropdownMenuItem<Object?>>[
      const DropdownMenuItem(value: null, child: Text('wie normal')),
      for (var x = 10; x <= 90; x += 10) DropdownMenuItem(value: -x, child: Text('$x % langsamer')),
      for (final (name, titel) in widget.andere) DropdownMenuItem(value: name, child: Text('wie „$titel“')),
    ];
    return ListTile(
      dense: true,
      title: Text(text),
      trailing: DropdownButton<Object?>(
        value: auswahl.any((a) => a.value == wert) ? wert : null,
        items: auswahl,
        onChanged: (v) => setState(() => v == null ? p.belagRegeln.remove(k) : p.belagRegeln[k] = v),
      ),
    );
  }

  void _belagHilfe() => showDialog<void>(
        context: context,
        builder: (ctx) => AlertDialog(
          title: const Text('Belag'),
          content: const Text('Stell je Belag ein, wie schnell du dort bist: wie normal, ein Prozent langsamer oder so schnell wie in einem anderen Profil, '
              'zum Beispiel „Zu Fuß“ auf Schotter.\n\n'
              'Die Route vergleicht dann den normalen Weg mit Wegen, die diese Beläge umgehen, und nimmt den schnellsten. Lohnt sich der Umweg nicht, bleibt es beim direkten Weg.\n\n'
              'Erde, Gras, Sand umfasst auch sehr holprige Wege, grobes Pflaster auch holprige Abschnitte. '
              'Bekannt ist nur, was in OpenStreetMap eingetragen ist.'),
          actions: [TextButton(onPressed: () => Navigator.pop(ctx), child: const Text('OK'))],
        ),
      );

  Widget _hangRegler(String titel, int wert, (int, int) b, void Function(int) setzen) => Padding(
        padding: const EdgeInsets.symmetric(horizontal: 16),
        child: Row(children: [
          Expanded(
            flex: 2,
            child: Text('$titel: ${wert == 0 ? 'wie im Schnitt' : '${wert.abs()} % ${wert < 0 ? 'langsamer' : 'schneller'}'}'),
          ),
          Expanded(
            flex: 3,
            child: Slider(
              value: wert.toDouble(),
              min: b.$1.toDouble(),
              max: b.$2.toDouble(),
              divisions: (b.$2 - b.$1) ~/ hangSchritt,
              onChanged: (v) => setState(() => setzen(v.round())),
            ),
          ),
        ]),
      );

  Widget _schalter(String titel, String k, {String? unter}) => SwitchListTile(
        title: Text(titel),
        subtitle: unter == null ? null : Text(unter),
        value: p.meiden[k] == true,
        onChanged: (v) => setState(() => p.setzen(k, v)),
      );

  @override
  Widget build(BuildContext context) {
    final rad = p.meiden['fusswege'] == true, auto = p.art == 'auto', oev = p.meiden['oev'] == true;
    final grenze = p.frei ? 'jedes Tempo über 0' : '${tempoText(p.min)} bis ${tempoText(p.max)}';
    return Scaffold(
      appBar: AppBar(
        title: Text(p.eigen ? 'Eigenes Profil' : p.anzeige),
        actions: [TextButton(onPressed: _ok ? _uebernehmen : null, child: const Text('Übernehmen'))],
      ),
      body: ListView(padding: const EdgeInsets.only(bottom: 24), children: [
        if (p.eigen)
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 12, 16, 0),
            child: TextField(
              controller: _name,
              maxLength: 24,
              decoration: const InputDecoration(labelText: 'Name', helperText: 'z. B. Rennrad, Inliner'),
              onChanged: (_) => setState(() {}),
            ),
          ),
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 12, 16, 0),
          child: TextField(
            controller: _tempo,
            keyboardType: const TextInputType.numberWithOptions(decimal: true),
            decoration: InputDecoration(labelText: auto ? 'Höchsttempo' : 'Tempo', suffixText: 'km/h', helperText: grenze),
            onChanged: (_) => setState(() {}),
          ),
        ),
        if (!auto) ...[
          const _Ueberschrift('Am Hang', 'Stellt der Messgang ein, von Hand änderbar. Bergauf wird es stufenlos langsamer'),
          if (p.eigen)
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 16),
              child: Row(children: [
                Expanded(flex: 2, child: Text(p.zuFussAb == 0 ? 'Zu Fuß ab: nie' : 'Zu Fuß ab ${p.zuFussAb} % Steigung')),
                Expanded(
                  flex: 3,
                  child: Slider(
                    value: p.zuFussAb.toDouble(),
                    max: zuFussMax.toDouble(),
                    divisions: zuFussMax,
                    onChanged: (v) => setState(() => p.zuFussAb = v.round()),
                  ),
                ),
              ]),
            ),
          if (p.zuFussAb == 0) _hangRegler('Bergauf', p.auf, aufBereich, (v) => p.auf = v),
          _hangRegler('Bergab', p.ab, abBereich, (v) => p.ab = v),
        ],
        const _Ueberschrift('Meiden', 'Links egal, rechts so weit wie möglich'),
        if (p.eigen)
          _schalter('Fußwege', 'fusswege', unter: 'Route über Straßen und Radwege; Fußwege nur, wenn es sonst keinen Weg gibt'),
        for (final (k, text, fuer) in _stufen)
          if (fuer.contains(p.art) && !(k == 'hauptstrassen' && !rad) && !(k == 'dunkel' && rad))
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 16),
              child: Row(children: [
                Expanded(flex: 2, child: Text(text)),
                Expanded(
                  flex: 3,
                  child: Slider(
                    value: p.stufe(k).clamp(0.0, 1.0),
                    divisions: 10,
                    onChanged: (v) => setState(() => p.setzen(k, v)),
                  ),
                ),
              ]),
            ),
        if (!auto) ...[
          Row(children: [
            const Expanded(child: _Ueberschrift('Belag', 'Wie schnell du dort bist. Die Route nimmt den schnellsten Weg')),
            IconButton(icon: const Icon(Icons.help_outline), tooltip: 'Was das bedeutet', onPressed: _belagHilfe),
          ]),
          for (final (k, text) in _belagArten) _belagZeile(k, text),
        ],
        if (!auto && !rad) ...[
          const _Ueberschrift('Wege'),
          _schalter('Anlieger-Zufahrten mitnehmen', 'anlieger', unter: 'Auch durch Zufahrten nur für Anlieger, wenn der Weg dadurch kürzer wird'),
        ],
        if (auto)
          SwitchListTile(
            title: const Text('Schlechter Belag (Schotter, Feldwege)'),
            subtitle: const Text('Nur, wenn es einen anderen Weg gibt'),
            value: p.stufe('belag') > 0,
            onChanged: (v) => setState(() => p.setzen('belag', v ? 1.0 : null)),
          ),
        if (!auto) ...[
          const _Ueberschrift('Bus und Bahn'),
          _schalter('Bus und Bahn ganz meiden', 'oev', unter: 'Nur der direkte Weg, auch über lange Strecken'),
          if (!oev) ...[
            _schalter('Busse meiden', 'bus', unter: 'Nur Schiene'),
            if (p.meiden['bus'] != true)
              Padding(
                padding: const EdgeInsets.symmetric(horizontal: 16),
                child: Row(children: [
                  Expanded(
                    flex: 2,
                    child: Text((p.meiden['bus_min'] as int? ?? 0) == 0
                        ? 'Bus muss nichts sparen'
                        : 'Bus nur, wenn er ${p.meiden['bus_min']} min spart'),
                  ),
                  Expanded(
                    flex: 3,
                    child: Slider(
                      value: (p.meiden['bus_min'] as int? ?? 0).toDouble(),
                      max: 30,
                      divisions: 30,
                      onChanged: (v) => setState(() => p.setzen('bus_min', v.round())),
                    ),
                  ),
                ]),
              ),
            ListTile(
              title: const Text('Umstiege'),
              trailing: DropdownButton<int?>(
                value: p.meiden['umstiege'] as int?,
                items: [
                  const DropdownMenuItem(value: null, child: Text('egal')),
                  for (final n in [0, 1, 2, 3]) DropdownMenuItem(value: n, child: Text('höchstens $n')),
                ],
                onChanged: (v) => setState(() => v == null ? p.meiden.remove('umstiege') : p.meiden['umstiege'] = v),
              ),
            ),
            SwitchListTile(
              title: const Text('Fernverkehr meiden'),
              subtitle: const Text('ICE, IC und andere, die das Deutschlandticket nicht abdeckt'),
              value: !p.fern,
              onChanged: (v) => setState(() => p.fern = !v),
            ),
            if (p.eigen)
              SwitchListTile(
                title: const Text('Fahrradticket nötig'),
                subtitle: const Text('Nur ein Hinweis in der App, der Server erfährt das nicht'),
                value: p.ticket,
                onChanged: (v) => setState(() => p.ticket = v),
              ),
          ],
        ],
      ]),
    );
  }
}

class _Ueberschrift extends StatelessWidget {
  final String text;
  final String? info;
  const _Ueberschrift(this.text, [this.info]);

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 4),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Text(text, style: t.textTheme.labelLarge?.copyWith(color: t.colorScheme.primary)),
        if (info != null) Text(info!, style: t.textTheme.bodySmall?.copyWith(color: t.colorScheme.onSurfaceVariant)),
      ]),
    );
  }
}

/// Der Messgang selbst; liefert beim Übernehmen das gemessene Tempo in km/h.
class MessSeite extends StatefulWidget {
  final TempoProfil profil;
  final String? fuss; // Schlüssel des Fuß-Profils: dort, wo gelaufen wird, gilt dessen Tempo
  const MessSeite({super.key, required this.profil, this.fuss});

  @override
  State<MessSeite> createState() => _MessSeiteState();
}

class _MessSeiteState extends State<MessSeite> {
  Timer? _takt;
  MessLeser? _leser;
  bool _liest = false, _laeuft = true, _pausiert = false, _wertFehler = false;
  Fix? _letzter;
  String? _fehler;
  bool _genau = false; // schon eine brauchbare Position?
  double _gehMs = 0, _strecke = 0, _jetzt = 0; // ms in Bewegung, m, m/s
  final _spur = <SpurPunkt>[]; // aus der Aufzeichnung des Dienstes
  bool _wertet = false;

  late final (double, double) _grenze = _bewegung[widget.profil.art]!;
  double get _kmh => _gehMs == 0 ? 0 : _strecke / _gehMs * 3600;
  bool get _reicht => _gehMs >= _zeitMin.inMilliseconds && _strecke >= _streckeMin;

  @override
  void initState() {
    super.initState();
    _starten();
  }

  Future<void> _starten() async {
    await Standort.holen(); // fragt bei Bedarf nach der Berechtigung
    final d = await Messung.datei();
    if (!mounted) return;
    _leser = MessLeser(d);
    if (!await d.exists()) await _aufnehmen(); // sonst geht es mit der angefangenen Messung weiter
    await _lesen();
    if (mounted) _takt = Timer.periodic(const Duration(seconds: 1), (_) => _lesen());
  }

  Future<void> _aufnehmen() async {
    try {
      await Messung.starten(widget.profil.name, widget.profil.anzeige);
      if (mounted) setState(() => _fehler = null);
    } on PlatformException catch (e) {
      if (!mounted) return;
      setState(() => _fehler = e.code == 'aus'
          ? 'GPS ist aus – bitte Standort einschalten.'
          : e.code == 'erlaubnis'
              ? 'Ohne Standort-Berechtigung geht die Messung nicht.'
              : 'GPS nicht verfügbar.');
    }
  }

  Future<void> _lesen() async {
    if (_liest || _leser == null) return;
    _liest = true;
    try {
      for (final x in await _leser!.neue()) {
        if (x is MessPause) {
          _pausiert = x.an;
          _letzter = null; // keine Strecke über die Pause hinweg
        } else if (x is Fix && !_pausiert) {
          _neu(x);
        }
      }
      final l = await Messung.laeuft();
      if (mounted) setState(() => _laeuft = l);
    } on Exception {
      // beim nächsten Takt wieder
    } finally {
      _liest = false;
    }
  }

  void _neu(Fix f) {
    if (f.genau > _genauMax) {
      _letzter = null; // keine Strecke über ungenaue Stellen hinweg
      return;
    }
    final a = _letzter;
    _letzter = f;
    _genau = true;
    if (a != null) {
      final dt = f.zeit - a.zeit;
      if (dt > 0 && dt <= _lueckeMax) {
        final v = f.tempo >= 0 ? f.tempo : _abstand(a, f) / dt * 1000;
        _jetzt = v;
        if (v >= _grenze.$1 && v <= _grenze.$2) {
          _gehMs += dt;
          _strecke += v * dt / 1000;
          if (_spur.length < _spurMax) _spur.add(SpurPunkt(f.lat, f.lon, v * dt / 1000, dt / 1000));
        }
      }
    }
  }

  @override
  void dispose() {
    _takt?.cancel(); // die Aufzeichnung läuft weiter
    super.dispose();
  }

  Future<void> _verwerfen() async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Messung verwerfen?'),
        content: const Text('Die bisher aufgezeichnete Strecke wird gelöscht.'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('Abbrechen')),
          FilledButton(onPressed: () => Navigator.pop(ctx, true), child: const Text('Verwerfen')),
        ],
      ),
    );
    if (ok != true) return;
    await Messung.verwerfen();
    if (mounted) Navigator.of(context).pop();
  }

  Future<void> _fertig({bool nurTempo = false}) async {
    final kmh = (_kmh * 10).round() / 10;
    final p = widget.profil;
    if (!p.erlaubt(kmh)) {
      setState(() => _fehler = 'Gemessen ${tempoText(kmh)} – für ${artName[p.art]} gehen ${tempoText(p.min)} bis ${tempoText(p.max)}.');
      return;
    }
    setState(() {
      _wertet = true;
      _wertFehler = false;
    });
    await Messung.anhalten();
    final e = nurTempo ? null : await hangMessen(_spur, kmh, eigen: p.eigen, fuss: widget.fuss);
    if (!mounted) return;
    if (e == null && !nurTempo) {
      setState(() {
        _wertet = false;
        _wertFehler = true; // die Aufzeichnung bleibt, zum Beispiel ohne Netz später noch einmal
      });
      return;
    }
    Navigator.of(context).pop((kmh, e));
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final name = widget.profil.anzeige, fuss = widget.profil.art == 'fuss';
    final fehlt = [
      if (_gehMs < _zeitMin.inMilliseconds) 'noch ${_dauer(_zeitMin.inMilliseconds - _gehMs)}',
      if (_strecke < _streckeMin) 'noch ${(_streckeMin - _strecke).ceil()} m',
    ];
    return Scaffold(
      appBar: AppBar(title: Text('Messen: $name')),
      body: ListView(padding: const EdgeInsets.all(16), children: [
        Text('${fuss ? 'Geh' : 'Fahr'} so, wie du „$name“ ${fuss ? 'gehst' : 'fährst'} – am besten draußen mit freiem Himmel. '
            'Die Messung läuft im Hintergrund weiter, der Bildschirm darf ausgehen und du kannst die App verlassen. '
            'Stehen an Ampeln zählt nicht mit. Bus und Bahn gehören nicht in die Messung: Drück dafür Pause, das geht auch in der Benachrichtigung. '
            'Nimm auch ein Stück bergauf und bergab und verschiedene Beläge mit, dann stellt die Messung das Tempo dafür selbst ein. '
            'Am Ende geht die Strecke einmal an deinen Server, er speichert sie nicht.'),
        const SizedBox(height: 24),
        if (_fehler != null)
          Text(_fehler!, style: TextStyle(color: t.colorScheme.error))
        else if (!_genau)
          const Row(children: [
            SizedBox.square(dimension: 18, child: CircularProgressIndicator(strokeWidth: 2)),
            SizedBox(width: 12),
            Text('GPS sucht …'),
          ])
        else ...[
          Center(child: Text(tempoText(_kmh), style: t.textTheme.displayMedium)),
          Center(child: Text('Durchschnitt in Bewegung', style: t.textTheme.bodySmall)),
          const SizedBox(height: 24),
          _Wert('Gerade', _jetzt >= _grenze.$1 ? tempoText(_jetzt * 3.6) : 'steht'),
          _Wert(fuss ? 'Gehzeit' : 'Fahrzeit', _dauer(_gehMs)),
          _Wert('Strecke', '${_strecke.round()} m'),
          if (fehlt.isNotEmpty) ...[
            const SizedBox(height: 8),
            LinearProgressIndicator(
                value: min(_gehMs / _zeitMin.inMilliseconds, _strecke / _streckeMin).clamp(0, 1).toDouble()),
            const SizedBox(height: 4),
            Text('Für ein brauchbares Ergebnis: ${fehlt.join(', ')}', style: t.textTheme.bodySmall),
          ],
        ],
        const SizedBox(height: 24),
        if (_wertFehler) ...[
          Text('Die Strecke ließ sich nicht auswerten, sie bleibt gespeichert. Hast du Netz?', style: TextStyle(color: t.colorScheme.error)),
          const SizedBox(height: 8),
        ] else if (!_laeuft && !_wertet) ...[
          const Text('Die Messung ist angehalten.'),
          const SizedBox(height: 8),
        ] else if (_pausiert) ...[
          const Text('Pausiert, nichts zählt mit.'),
          const SizedBox(height: 8),
        ],
        FilledButton(
          onPressed: _reicht && !_wertet ? _fertig : null,
          child: Text(_wertet ? 'Strecke wird ausgewertet …' : _wertFehler ? 'Noch einmal auswerten' : _reicht ? '${tempoText(_kmh)} für „$name“ übernehmen' : 'Fertig'),
        ),
        if (_wertFehler && !_wertet)
          TextButton(onPressed: () => _fertig(nurTempo: true), child: const Text('Nur das Tempo übernehmen')),
        const SizedBox(height: 8),
        if (!_laeuft)
          OutlinedButton(onPressed: _wertet ? null : _aufnehmen, child: const Text('Weiter messen'))
        else
          OutlinedButton(onPressed: _wertet ? null : () => Messung.pausieren(!_pausiert), child: Text(_pausiert ? 'Weiter' : 'Pause')),
        TextButton(onPressed: _wertet ? null : _verwerfen, child: const Text('Messung verwerfen')),
      ]),
    );
  }
}

class _Wert extends StatelessWidget {
  final String name, wert;
  const _Wert(this.name, this.wert);

  @override
  Widget build(BuildContext context) => Padding(
        padding: const EdgeInsets.symmetric(vertical: 4),
        child: Row(children: [Expanded(child: Text(name)), Text(wert)]),
      );
}

String _dauer(double ms) {
  final s = (ms / 1000).round();
  return '${s ~/ 60}:${(s % 60).toString().padLeft(2, '0')} min';
}

/// Abstand in Metern (Haversine).
double _abstand(Position a, Position b) {
  const r = 6371000.0, rad = pi / 180;
  final dLat = (b.lat - a.lat) * rad, dLon = (b.lon - a.lon) * rad;
  final h = pow(sin(dLat / 2), 2) + cos(a.lat * rad) * cos(b.lat * rad) * pow(sin(dLon / 2), 2);
  return 2 * r * asin(sqrt(h));
}
