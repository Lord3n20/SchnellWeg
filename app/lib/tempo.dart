// Gehtempo (D-12): Standard gemütlich 4,3 km/h, einstellbar 2–8 km/h (Grenze von /v1/route).
// Bleibt nur auf dem Handy (kleine Datei im App-Ordner) und geht je Anfrage als `kmh` mit.
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:path_provider/path_provider.dart';

const standardKmh = 4.3;
const _min = 2.0, _max = 8.0;
const _stufen = {'gemütlich': standardKmh, 'normal': 5.0, 'schnell': 6.0};

class Tempo {
  static Future<File> _datei() async => File('${(await getApplicationSupportDirectory()).path}/tempo');

  static Future<double> laden() async {
    try {
      final v = double.tryParse(await (await _datei()).readAsString());
      if (v != null && v >= _min && v <= _max) return v;
    } on FileSystemException {
      // noch nie gesetzt
    }
    return standardKmh;
  }

  static Future<void> speichern(double kmh) async => (await _datei()).writeAsString(kmh.toStringAsFixed(1));
}

String tempoText(double kmh) => '${kmh.toStringAsFixed(1).replaceAll('.', ',')} km/h';

/// Auswahl als Bottom Sheet; liefert das neue Tempo oder null, wenn nichts geändert wurde.
Future<double?> tempoWaehlen(BuildContext context, double jetzt) => showModalBottomSheet<double>(
      context: context,
      builder: (_) => _TempoBlatt(jetzt),
    );

class _TempoBlatt extends StatefulWidget {
  final double start;
  const _TempoBlatt(this.start);

  @override
  State<_TempoBlatt> createState() => _TempoBlattState();
}

class _TempoBlattState extends State<_TempoBlatt> {
  late double _kmh = widget.start;

  @override
  Widget build(BuildContext context) => SafeArea(
        child: Padding(
          padding: const EdgeInsets.fromLTRB(16, 16, 16, 8),
          child: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.start, children: [
            Text('Gehtempo: ${tempoText(_kmh)}', style: Theme.of(context).textTheme.titleMedium),
            Slider(
              value: _kmh,
              min: _min,
              max: _max,
              divisions: ((_max - _min) * 10).round(),
              label: tempoText(_kmh),
              onChanged: (v) => setState(() => _kmh = v),
            ),
            Wrap(spacing: 8, children: [
              for (final MapEntry(key: name, value: v) in _stufen.entries)
                ChoiceChip(
                  label: Text('$name ${tempoText(v)}'),
                  selected: (_kmh - v).abs() < 0.05,
                  onSelected: (_) => setState(() => _kmh = v),
                ),
            ]),
            const SizedBox(height: 8),
            Align(
              alignment: Alignment.centerRight,
              child: FilledButton(
                onPressed: () => Navigator.of(context).pop((_kmh - widget.start).abs() < 0.05 ? null : _kmh),
                child: const Text('Übernehmen'),
              ),
            ),
          ]),
        ),
      );
}
