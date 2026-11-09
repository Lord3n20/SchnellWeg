// Kleine Abkürzungen (D-47): Verbindung als Text teilen, Rückweg (Start und Ziel tauschen) und Favoriten als
// Kurzbefehle am App-Icon. Alles bleibt auf dem Handy, nur das Teilen übergibt Text an die App, die man wählt.
import 'package:flutter/services.dart';

import 'api.dart';
import 'namen.dart';
import 'standort.dart';

String _uhr(DateTime t) => '${t.hour.toString().padLeft(2, '0')}:${t.minute.toString().padLeft(2, '0')}';

/// Text zum Weiterschicken: Ankunft zuerst, darunter jede Fahrt mit Echtzeit, wenn bekannt. Der Startort kommt nicht vor.
String verbindungText(Verbindung v, String zielName) => [
      'Ankunft ${_uhr(v.ankunft)} · $zielName',
      'Los ${_uhr(v.ab)} · ${minuten(v.ankunft.difference(v.ab))} min · ${v.artText}',
      for (final a in v.abschnitte)
        if (a.fahrt)
          '${_uhr(a.abRt ?? a.ab)} ${a.linie}: ${haltName(a.von?.name ?? '')} → ${haltName(a.nach?.name ?? '')}, '
              'an ${_uhr(a.anRt ?? a.an)}${a.faelltAus ? ' (fällt aus)' : ''}',
    ].join('\n');

/// Android-Auswahl „Teilen“ mit [text].
Future<void> teilen(String text) => const MethodChannel('schnellweg/teilen').invokeMethod('teilen', text);

/// Art eines Orts, der nur für den Rückweg aus dem eigenen Standort entsteht.
const artHier = 'standort';

/// Start und Ziel tauschen: gibt (neuer Start, neues Ziel) zurück, Start null = eigener Standort.
/// null, wenn der Rückweg zum eigenen Standort führen soll, der aber noch nicht bekannt ist.
(Ort?, Ort)? tauschen(Ort? start, Ort ziel, Position? hier) {
  final neu = start ?? (hier == null ? null : Ort(artHier, 'Mein Standort', '', hier.lat, hier.lon));
  if (neu == null) return null;
  return (ziel.art == artHier ? null : ziel, neu);
}

/// Favoriten beim langen Drücken auf das App-Icon.
class Kurzbefehle {
  static const _kanal = MethodChannel('schnellweg/kurzbefehl');

  static Future<void> setzen(List<Ort> orte) async {
    await _kanal.invokeMethod('setzen', [
      for (final o in orte) {'art': o.art, 'name': o.name, 'detail': o.detail, 'lat': o.lat, 'lon': o.lon}
    ]);
  }

  /// Der Kurzbefehl, mit dem die App gerade geöffnet wurde; einmalig, danach null.
  static Future<Ort?> abholen() async {
    final m = await _kanal.invokeMapMethod<String, dynamic>('abholen');
    return m == null ? null : Ort(m['art'] as String, m['name'] as String, m['detail'] as String, m['lat'] as double, m['lon'] as double);
  }

  /// [neu] läuft, wenn ein Kurzbefehl die schon offene App trifft.
  static void beiNeu(void Function() neu) => _kanal.setMethodCallHandler((c) async {
        if (c.method == 'neu') neu();
      });
}
