// Tempo-Profile verwalten und per Messgang kalibrieren: ein paar Minuten im Profiltempo gehen oder fahren, die App
// mittelt das GPS-Tempo über die Zeit in Bewegung (Stehen an Ampeln zählt nicht). Positionen bleiben im Speicher
// der Seite und verschwinden mit ihr; gespeichert wird nur das Ergebnis.
import 'dart:async';
import 'dart:math';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'hang_messung.dart';
import 'standort.dart';
import 'tempo.dart';

const _genauMax = 20.0; // m; ungenauere Positionen zählen nicht
/// Art → (min, max) m/s in Bewegung; darunter Stehen, darüber etwas anderes (Rennen, Bus, Auto).
const _bewegung = {
  'fuss': (0.6, 3.0),
  'eigen': (1.0, 14.0),
};
const _belagUmwegVorgabe = 50; // % wie der Server ohne Angabe
const _spurMax = 20000;
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

  @override
  void initState() {
    super.initState();
    Tempo.oeffnen().then((t) {
      if (mounted) setState(() => _t = t);
    });
  }

  Future<void> _aendern(void Function(Tempo) f) async {
    setState(() => f(_t!));
    await _t!.speichern();
  }

  Future<void> _messen(TempoProfil p) async {
    final r = await Navigator.of(context).push<(double, HangErgebnis?)>(MaterialPageRoute(builder: (_) => MessSeite(profil: p)));
    if (r == null) return;
    final (kmh, e) = r;
    await _aendern((_) {
      p.kmh = e != null && p.erlaubt(e.kmhEben) ? e.kmhEben : kmh;
      p.gemessen = DateTime.now();
      if (e != null) {
        p.auf = e.auf ?? p.auf;
        p.ab = e.ab ?? p.ab;
        p.zuFussAb = e.zuFussAb ?? p.zuFussAb;
      }
    });
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(_messText(p, e))));
  }

  /// Bearbeiten oder (neu) anlegen; gespeichert wird erst beim Übernehmen.
  Future<void> _bearbeiten(TempoProfil p, {bool neu = false}) async {
    final x = await Navigator.of(context).push<TempoProfil>(MaterialPageRoute(builder: (_) => ProfilSeite(p.kopie(), neu: neu)));
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
                subtitle: Text(!p.messbar ? 'höchstens ${tempoText(p.kmh)}' : '${tempoText(p.kmh)} · '
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

/// Welche Beläge die Route bei dieser Stufe umgeht (D-44), jede Stufe nimmt die vorigen mit.
String _belagText(double v) => switch ((v * 5 + 1e-9).floor()) {
      0 => 'egal',
      1 => 'Erde, Gras, Sand',
      2 => '+ Schotter',
      3 => '+ grobes Pflaster',
      4 => '+ Kopfsteinpflaster',
      _ => '+ Gehwegplatten',
    };

String _messText(TempoProfil p, HangErgebnis? e) {
  if (e == null) return 'Tempo gespeichert. Die Steigung ließ sich nicht auswerten, die Hang-Regler bleiben.';
  if (e.auf == null && e.ab == null && e.zuFussAb == null) return 'Tempo gespeichert. Für die Hang-Regler war zu wenig Steigung dabei.';
  return 'Gemessen: eben ${tempoText(p.kmh)}, '
      '${p.zuFussAb > 0 ? 'zu Fuß ab ${p.zuFussAb} %' : 'bergauf ${p.auf} %'}, bergab ${p.ab > 0 ? '+' : ''}${p.ab} %';
}

/// Stufen, was die Route meiden soll: Schlüssel wie in /v1/route, Text, für welche Profile.
const _stufen = [
  ('treppen', 'Treppen', {'fuss', 'eigen'}),
  ('belag', 'Schlechter Belag', {'fuss', 'eigen'}),
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
  const ProfilSeite(this.p, {super.key, this.neu = false});

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

  Widget _belagUmwegRegler() {
    final w = (p.meiden['belag_umweg'] as num?)?.toInt() ?? _belagUmwegVorgabe;
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 16),
      child: Row(children: [
        Expanded(flex: 2, child: Text('Wie sehr: bis $w % Umweg')),
        Expanded(
          flex: 3,
          child: Slider(
            value: w.clamp(10, 200).toDouble(),
            min: 10,
            max: 200,
            divisions: 19,
            onChanged: (v) => setState(() => p.meiden['belag_umweg'] = v.round()),
          ),
        ),
      ]),
    );
  }

  void _belagHilfe() => showDialog<void>(
        context: context,
        builder: (ctx) => AlertDialog(
          title: const Text('Schlechter Belag'),
          content: const Text('Der Regler legt fest, was die Route umgeht. Jede Stufe nimmt die vorigen mit:\n\n'
              '1  Erde, Gras, Sand, sehr holprige Wege\n'
              '2  + Schotter\n'
              '3  + grobes Pflaster und holprige Abschnitte\n'
              '4  + Kopfsteinpflaster\n'
              '5  + Gehwegplatten\n\n'
              '„Wie sehr“ sagt, wie viel länger der Weg dafür werden darf. Wird er länger, bleibt es beim besten Weg innerhalb der Grenze.\n\n'
              'Das Ziel und der Start selbst lassen sich nicht umgehen, und bekannt ist nur, was in OpenStreetMap eingetragen ist.'),
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
            ...[
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 16),
              child: Row(children: [
                Expanded(flex: 2, child: Text(k == 'belag' ? '$text: ${_belagText(p.stufe(k))}' : text)),
                if (k == 'belag')
                  IconButton(icon: const Icon(Icons.help_outline), tooltip: 'Was die Stufen bedeuten', onPressed: _belagHilfe),
                Expanded(
                  flex: 3,
                  child: Slider(
                    value: p.stufe(k).clamp(0.0, 1.0),
                    divisions: k == 'belag' ? 5 : 10,
                    onChanged: (v) => setState(() => p.setzen(k, v)),
                  ),
                ),
              ]),
            ),
            if (k == 'belag' && p.stufe(k) >= 0.2) _belagUmwegRegler(),
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
  const MessSeite({super.key, required this.profil});

  @override
  State<MessSeite> createState() => _MessSeiteState();
}

class _MessSeiteState extends State<MessSeite> {
  StreamSubscription<Fix>? _abo;
  Fix? _letzter;
  String? _fehler;
  bool _genau = false; // schon eine brauchbare Position?
  double _gehMs = 0, _strecke = 0, _jetzt = 0; // ms in Bewegung, m, m/s
  final _spur = <SpurPunkt>[]; // bleibt auf dem Handy und verschwindet mit der Seite
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
    if (!mounted) return;
    _abo = Standort.laufend().listen(_neu, onError: (Object e) {
      final art = e is PlatformException ? e.code : '';
      setState(() => _fehler = art == 'aus'
          ? 'GPS ist aus – bitte Standort einschalten.'
          : art == 'erlaubnis'
              ? 'Ohne Standort-Berechtigung geht die Messung nicht.'
              : 'GPS nicht verfügbar.');
    });
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
    setState(() {});
  }

  @override
  void dispose() {
    _abo?.cancel();
    super.dispose();
  }

  Future<void> _fertig() async {
    final kmh = (_kmh * 10).round() / 10;
    final p = widget.profil;
    if (!p.erlaubt(kmh)) {
      setState(() => _fehler = 'Gemessen ${tempoText(kmh)} – für ${artName[p.art]} gehen ${tempoText(p.min)} bis ${tempoText(p.max)}.');
      return;
    }
    setState(() => _wertet = true);
    final e = await hangMessen(_spur, kmh, eigen: p.eigen);
    if (mounted) Navigator.of(context).pop((kmh, e));
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
            'Lass die App offen, der Bildschirm bleibt an${fuss ? '' : ' (Handy sicher verstauen oder in die Halterung)'}. '
            'Stehen an Ampeln zählt nicht mit. '
            'Nimm auch ein Stück bergauf und bergab mit, dann stellt die Messung die Hang-Regler selbst ein.'),
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
        FilledButton(
          onPressed: _reicht && !_wertet ? _fertig : null,
          child: Text(_wertet ? 'Steigung wird ausgewertet …' : _reicht ? '${tempoText(_kmh)} für „$name“ übernehmen' : 'Fertig'),
        ),
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
