// Zugang einrichten: Server-Adresse + p12-Client-Zertifikat. Wird vor dem Speichern mit /v1/status geprüft.
import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';

import 'api.dart';
import 'zugang.dart';

class Einstellungen extends StatefulWidget {
  final Zugang? alt;
  const Einstellungen({super.key, this.alt});

  @override
  State<Einstellungen> createState() => _EinstellungenState();
}

class _EinstellungenState extends State<Einstellungen> {
  late final _url = TextEditingController(text: widget.alt?.basis.toString() ?? 'https://');
  late final _pw = TextEditingController(text: widget.alt?.passwort ?? '');
  late List<int>? _p12 = widget.alt?.p12;
  String? _p12Name, _meldung;
  bool _laeuft = false;

  Future<void> _waehlen() async {
    final f = await FilePicker.pickFile();
    if (f == null) return;
    final daten = await f.readAsBytes();
    setState(() {
      _p12 = daten;
      _p12Name = f.name;
    });
  }

  Future<void> _speichern() async {
    final url = Uri.tryParse(_url.text.trim());
    if (url == null || url.scheme != 'https' || url.host.isEmpty) {
      return setState(() => _meldung = 'Adresse muss mit https:// beginnen');
    }
    if (_p12 == null) return setState(() => _meldung = 'Zertifikat fehlt');
    setState(() {
      _laeuft = true;
      _meldung = null;
    });
    final z = Zugang(url, _p12!, _pw.text);
    String? fehler;
    try {
      final api = Api(z);
      try {
        await api.status();
      } finally {
        api.schliessen();
      }
    } on ApiFehler catch (e) {
      fehler = switch (e.art) {
        'zertifikat' => 'Zertifikat abgelehnt oder Passwort falsch',
        'netz' => 'Server nicht erreichbar',
        _ => 'Server meldet: ${e.art}',
      };
    } catch (_) {
      fehler = 'Zertifikat lässt sich nicht öffnen (Passwort?)';
    }
    if (!mounted) return;
    if (fehler != null) {
      return setState(() {
        _laeuft = false;
        _meldung = fehler;
      });
    }
    await z.speichern();
    if (mounted) Navigator.of(context).pop(z);
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Zugang')),
      body: ListView(padding: const EdgeInsets.all(16), children: [
        TextField(
          controller: _url,
          keyboardType: TextInputType.url,
          autocorrect: false,
          decoration: const InputDecoration(labelText: 'Server-Adresse', border: OutlineInputBorder()),
        ),
        const SizedBox(height: 16),
        OutlinedButton.icon(
          onPressed: _laeuft ? null : _waehlen,
          icon: const Icon(Icons.key),
          label: Text(_p12Name ?? (_p12 == null ? 'Client-Zertifikat wählen (.p12)' : 'Zertifikat gespeichert – ersetzen')),
        ),
        const SizedBox(height: 16),
        TextField(
          controller: _pw,
          obscureText: true,
          decoration: const InputDecoration(labelText: 'Passwort des Zertifikats', border: OutlineInputBorder()),
        ),
        const SizedBox(height: 24),
        FilledButton(
          onPressed: _laeuft ? null : _speichern,
          child: _laeuft
              ? const SizedBox.square(dimension: 18, child: CircularProgressIndicator(strokeWidth: 2))
              : const Text('Prüfen und speichern'),
        ),
        if (_meldung != null)
          Padding(
            padding: const EdgeInsets.only(top: 16),
            child: Text(_meldung!, style: TextStyle(color: Theme.of(context).colorScheme.error)),
          ),
      ]),
    );
  }
}
