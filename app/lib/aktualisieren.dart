// Selbstupdate (D-42): fragt beim Start den eigenen Server nach einer neueren Fassung und zeigt sie nur in den
// Einstellungen an. Geladen wird erst auf Knopfdruck, die Installation bestätigt der Nutzer im Dialog von Android. Die App-Daten bleiben.
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:path_provider/path_provider.dart';

import 'api.dart';
import 'karte.dart' show groesse;

/// Nur ein höherer versionCode gilt als Update, nie zurück auf eine ältere Fassung.
bool istNeuer(AppStand server, int installiert) => server.code > installiert;

class Aktualisieren extends ChangeNotifier {
  Aktualisieren._();
  static final instanz = Aktualisieren._();
  static const _kanal = MethodChannel('schnellweg/update');

  Api? _api;
  String? version;
  int? _code;
  AppStand? neu;
  bool sucht = false, laedt = false, bestaetigen = false, erlaubnisFehlt = false;
  double? anteil;
  String? fehler;

  String get text {
    if (neu != null) return '${neu!.version} verfügbar';
    return version == null ? 'Version unbekannt' : 'Version $version';
  }

  Future<void> start(Api api) async {
    _api = api;
    try {
      final s = await _kanal.invokeMapMethod<String, dynamic>('stand');
      version = s!['version'] as String?;
      _code = (s['code'] as num).toInt();
    } on PlatformException {
      return;
    } on MissingPluginException {
      return;
    }
    await pruefen();
  }

  Future<void> pruefen() async {
    final api = _api, code = _code;
    if (api == null || code == null || sucht || laedt) return;
    sucht = true;
    fehler = null;
    notifyListeners();
    try {
      final s = await api.appStand();
      neu = s != null && istNeuer(s, code) ? s : null;
      await _aufraeumen();
    } on ApiFehler catch (e) {
      fehler = 'Server nicht erreichbar ($e)';
    } finally {
      sucht = false;
      notifyListeners();
    }
  }

  Future<Directory> _ordner() async => Directory('${(await getTemporaryDirectory()).path}/update');

  String _name(AppStand s) => 'schnellweg-${s.code}.apk';

  // Alte APKs nicht liegen lassen (rund 20 MB). Nur nach erfolgreicher Prüfung, ein angefangener Download bleibt.
  Future<void> _aufraeumen() async {
    final d = await _ordner();
    if (!await d.exists()) return;
    await for (final f in d.list()) {
      final name = f.uri.pathSegments.last;
      if (neu == null || !name.startsWith(_name(neu!))) await f.delete();
    }
  }

  Future<void> installieren() async {
    final api = _api, s = neu;
    if (api == null || s == null || laedt) return;
    fehler = null;
    bestaetigen = false;
    erlaubnisFehlt = false;
    try {
      if (await _kanal.invokeMethod<bool>('erlaubt') != true) {
        erlaubnisFehlt = true;
        notifyListeners();
        await _kanal.invokeMethod<void>('erlaubnis');
        return;
      }
      laedt = true;
      anteil = 0;
      notifyListeners();
      final d = await _ordner();
      await d.create(recursive: true);
      final apk = File('${d.path}/${_name(s)}');
      if (!await apk.exists() || await apk.length() != s.bytes) {
        await api.apk(apk, fortschritt: (n, gesamt) {
          anteil = gesamt > 0 ? n / gesamt : null;
          notifyListeners();
        });
      }
      anteil = null;
      notifyListeners();
      await _kanal.invokeMethod<void>('installieren', {'pfad': apk.path, 'sha256': s.sha256});
      bestaetigen = true;
    } on ApiFehler catch (e) {
      fehler = 'Download fehlgeschlagen ($e), nochmal versuchen setzt ihn fort';
    } on PlatformException catch (e) {
      fehler = e.message == 'pruefsumme'
          ? 'Die geladene Datei war beschädigt und wurde gelöscht, bitte nochmal versuchen'
          : 'Installation fehlgeschlagen (${e.message})';
    } finally {
      laedt = false;
      notifyListeners();
    }
  }
}

class AktualisierenSeite extends StatelessWidget {
  const AktualisierenSeite({super.key});

  @override
  Widget build(BuildContext context) {
    final u = Aktualisieren.instanz;
    return Scaffold(
      appBar: AppBar(title: const Text('App-Update')),
      body: ListenableBuilder(
        listenable: u,
        builder: (context, _) {
          final neu = u.neu;
          return ListView(padding: const EdgeInsets.all(16), children: [
            Text('Installiert: ${u.version ?? 'unbekannt'}', style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 8),
            if (neu == null)
              Text(u.sucht ? 'Suche nach Updates …' : 'Kein neueres Update auf dem Server.')
            else
              Text('Neu: ${neu.version} (${groesse(neu.bytes)}). Deine Daten bleiben erhalten. '
                  'Die App startet bei der Installation neu.'),
            if (neu != null && (neu.hinweis ?? '').isNotEmpty) ...[
              const SizedBox(height: 16),
              Text('Was ist neu', style: Theme.of(context).textTheme.titleSmall),
              const SizedBox(height: 4),
              Text(neu.hinweis!),
            ],
            if (u.laedt) ...[
              const SizedBox(height: 16),
              LinearProgressIndicator(value: u.anteil),
            ],
            if (u.erlaubnisFehlt) ...[
              const SizedBox(height: 16),
              const Text('Erlaube SchnellWeg in den Android-Einstellungen, Apps zu installieren, '
                  'komm zurück und tippe nochmal auf „Installieren“.'),
            ],
            if (u.bestaetigen) ...[
              const SizedBox(height: 16),
              const Text('Bestätige die Installation im Dialog von Android. Abgebrochen? Dann tippe nochmal auf „Installieren“.'),
            ],
            if (u.fehler != null) ...[
              const SizedBox(height: 16),
              Text(u.fehler!, style: TextStyle(color: Theme.of(context).colorScheme.error)),
            ],
            const SizedBox(height: 24),
            if (neu != null)
              FilledButton.icon(
                onPressed: u.laedt ? null : u.installieren,
                icon: const Icon(Icons.system_update),
                label: Text(u.laedt ? 'Lädt …' : 'Installieren'),
              )
            else
              OutlinedButton(onPressed: u.sucht ? null : u.pruefen, child: const Text('Nach Updates suchen')),
          ]);
        },
      ),
    );
  }
}
