// Tempo-Profile verwalten und per Messgang kalibrieren: ein paar Minuten im Profiltempo gehen oder fahren, die App
// mittelt das GPS-Tempo über die Zeit in Bewegung (Stehen an Ampeln zählt nicht). Positionen bleiben im Speicher
// der Seite und verschwinden mit ihr; gespeichert wird nur das Ergebnis.
import 'dart:async';
import 'dart:math';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'standort.dart';
import 'tempo.dart';

const _genauMax = 20.0; // m; ungenauere Positionen zählen nicht
/// Art → (min, max) m/s in Bewegung; darunter Stehen, darüber etwas anderes (Rennen, Bus, Auto).
const _bewegung = {
  'fuss': (0.6, 3.0),
  'einrad': (1.0, 7.0),
  'klapprad': (1.5, 10.0),
  'fixie': (1.5, 13.0),
  'rennrad': (2.0, 14.0),
  'mtb': (1.5, 11.0),
};
const _lueckeMax = 5000.0; // ms; größere Lücken im GPS werden nicht überbrückt
const _zeitMin = Duration(minutes: 2), _streckeMin = 150.0;

/// Unterpunkt „Tempo“: Profile auswählen, von Hand setzen oder messen.
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
    Tempo.oeffnen().then((t) => setState(() => _t = t));
  }

  Future<void> _aendern(void Function(Tempo) f) async {
    setState(() => f(_t!));
    await _t!.speichern();
  }

  Future<void> _messen(TempoProfil p) async {
    final kmh = await Navigator.of(context).push<double>(MaterialPageRoute(builder: (_) => MessSeite(profil: p)));
    if (kmh == null) return;
    await _aendern((_) {
      p.kmh = kmh;
      p.gemessen = DateTime.now();
    });
  }

  Future<void> _vonHand(TempoProfil p) async {
    final kmh = await tempoWaehlen(context, p);
    if (kmh == null) return;
    await _aendern((_) {
      p.kmh = kmh;
      p.gemessen = null;
    });
  }

  @override
  Widget build(BuildContext context) {
    final t = _t;
    return Scaffold(
      appBar: AppBar(title: const Text('Tempo')),
      body: t == null
          ? const Center(child: CircularProgressIndicator())
          : ListView(children: [
              Padding(
                padding: const EdgeInsets.fromLTRB(16, 16, 16, 8),
                child: Text('Das ausgewählte Profil gilt für Wege zu Haltestellen, Umstiege und direkte Wege; '
                    'mit dem Rad steht der direkte Weg vorn. '
                    'Zum Kalibrieren ein Profil messen: ein paar Minuten in diesem Tempo unterwegs sein.'),
              ),
              for (final art in grenzen.keys) ...[
                _Ueberschrift(artName[art]!, artInfo[art]),
                for (final p in t.profile.where((p) => p.art == art)) ListTile(
                  leading: Icon(p.name == t.aktiv ? Icons.radio_button_checked : Icons.radio_button_unchecked,
                      color: p.name == t.aktiv ? Theme.of(context).colorScheme.primary : null),
                  title: Text(p.name),
                  subtitle: Text('${tempoText(p.kmh)} · '
                      '${p.gemessen == null ? 'nicht gemessen' : 'gemessen am ${p.gemessen!.day}.${p.gemessen!.month}.'}'),
                  onTap: () => _aendern((t) => t.aktiv = p.name),
                  trailing: PopupMenuButton<String>(
                    onSelected: (w) => w == 'messen' ? _messen(p) : _vonHand(p),
                    itemBuilder: (_) => const [
                      PopupMenuItem(value: 'messen', child: ListTile(leading: Icon(Icons.gps_fixed), title: Text('Messen'))),
                      PopupMenuItem(value: 'hand', child: ListTile(leading: Icon(Icons.tune), title: Text('Von Hand einstellen'))),
                    ],
                  ),
                ),
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

  void _fertig() {
    final kmh = (_kmh * 10).round() / 10;
    final p = widget.profil;
    if (kmh < p.min || kmh > p.max) {
      setState(() => _fehler = 'Gemessen ${tempoText(kmh)} – für ${artName[p.art]} gehen ${tempoText(p.min)} bis ${tempoText(p.max)}.');
      return;
    }
    Navigator.of(context).pop(kmh);
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final name = widget.profil.name, fuss = widget.profil.art == 'fuss';
    final fehlt = [
      if (_gehMs < _zeitMin.inMilliseconds) 'noch ${_dauer(_zeitMin.inMilliseconds - _gehMs)}',
      if (_strecke < _streckeMin) 'noch ${(_streckeMin - _strecke).ceil()} m',
    ];
    return Scaffold(
      appBar: AppBar(title: Text('Messen: $name')),
      body: ListView(padding: const EdgeInsets.all(16), children: [
        Text('${fuss ? 'Geh' : 'Fahr'} so, wie du „$name“ ${fuss ? 'gehst' : 'fährst'} – am besten draußen mit freiem Himmel. '
            'Lass die App offen, der Bildschirm bleibt an${fuss ? '' : ' (Handy sicher verstauen oder in die Halterung)'}. '
            'Stehen an Ampeln zählt nicht mit.'),
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
          onPressed: _reicht ? _fertig : null,
          child: Text(_reicht ? '${tempoText(_kmh)} für „$name“ übernehmen' : 'Fertig'),
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
