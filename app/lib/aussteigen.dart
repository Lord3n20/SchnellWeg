// Ausstiegs-Erinnerung (D-23): übergibt die Fahrten einer Verbindung an den AusstiegDienst (AusstiegDienst.kt),
// der per GPS auf dem Handy die Halte mitzählt und vor dem Ausstieg vibriert. Der Server erfährt davon nichts.
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'api.dart';
import 'namen.dart';

const _kanal = MethodChannel('schnellweg/aussteigen');

Future<bool> erinnerungLaeuft() async => await _kanal.invokeMethod<bool>('laeuft') ?? false;

/// Startet die Erinnerung für alle Fahrten von [v]; false, wenn Halte ohne Koordinaten sind oder die
/// Standort-Erlaubnis fehlt.
Future<bool> erinnerungStarten(Verbindung v, Map<String, Halt> halte) async {
  final fahrten = <Map<String, Object>>[];
  for (final a in v.abschnitte.where((a) => a.fahrt)) {
    final hs = [for (final id in a.halte) halte[id]];
    if (hs.length < 2 || hs.contains(null)) return false;
    fahrten.add({
      'linie': a.linie,
      'richtung': zielName(a.ziel),
      'halte': [for (final h in hs) {'name': haltName(h!.name), 'lat': h.lat, 'lon': h.lon}],
      'ankunft': (a.anRt ?? a.an).millisecondsSinceEpoch,
    });
  }
  if (fahrten.isEmpty) return false;
  try {
    await _kanal.invokeMethod<void>('starten', {'fahrten': fahrten});
    return true;
  } on PlatformException {
    return false;
  }
}

Future<void> erinnerungBeenden() => _kanal.invokeMethod<void>('beenden');

/// Glocke in der AppBar der Detailansicht: an/aus.
class ErinnerungKnopf extends StatefulWidget {
  final Verbindung v;
  final Map<String, Halt> halte;
  const ErinnerungKnopf({super.key, required this.v, required this.halte});

  @override
  State<ErinnerungKnopf> createState() => _ErinnerungKnopfState();
}

class _ErinnerungKnopfState extends State<ErinnerungKnopf> {
  bool _an = false;

  @override
  void initState() {
    super.initState();
    erinnerungLaeuft().then((an) {
      if (mounted) setState(() => _an = an);
    });
  }

  Future<void> _umschalten() async {
    final bote = ScaffoldMessenger.of(context);
    if (_an) {
      await erinnerungBeenden();
      setState(() => _an = false);
      bote.showSnackBar(const SnackBar(content: Text('Ausstiegs-Erinnerung aus')));
      return;
    }
    final ok = await erinnerungStarten(widget.v, widget.halte);
    if (!mounted) return;
    setState(() => _an = ok);
    bote.showSnackBar(SnackBar(
        content: Text(ok
            ? 'Erinnerung an: zählt die Halte per GPS mit und vibriert vor dem Aussteigen'
            : 'Geht nicht: Standort-Erlaubnis fehlt oder Halte unbekannt')));
  }

  @override
  Widget build(BuildContext context) => IconButton(
        icon: Icon(_an ? Icons.notifications_active : Icons.notifications_none),
        tooltip: _an ? 'Ausstiegs-Erinnerung beenden' : 'An den Ausstieg erinnern',
        onPressed: _umschalten,
      );
}
