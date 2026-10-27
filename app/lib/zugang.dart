// Zugang zum eigenen Server: Adresse + Client-Zertifikat (mTLS, D-13).
// Beides gibt der Nutzer in der App ein; nichts davon steht im Repo. Gespeichert im Android-Keystore
// (flutter_secure_storage), das Zertifikat als Base64 der p12-Datei.
import 'dart:convert';
import 'dart:io';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';

class Zugang {
  final Uri basis;
  final List<int> p12;
  final String passwort;

  Zugang(this.basis, this.p12, this.passwort);

  static const _speicher = FlutterSecureStorage();

  static Future<Zugang?> laden() async {
    final werte = await _speicher.readAll();
    final url = werte['url'], p12 = werte['p12'], pw = werte['p12_passwort'];
    if (url == null || p12 == null || pw == null) return null;
    return Zugang(Uri.parse(url), base64Decode(p12), pw);
  }

  Future<void> speichern() async {
    await _speicher.write(key: 'url', value: basis.toString());
    await _speicher.write(key: 'p12', value: base64Encode(p12));
    await _speicher.write(key: 'p12_passwort', value: passwort);
  }

  static Future<void> loeschen() => _speicher.deleteAll();

  /// HttpClient mit Client-Zertifikat; wirft bei falschem Passwort oder kaputter Datei.
  HttpClient client() {
    final ctx = SecurityContext(withTrustedRoots: true)
      ..useCertificateChainBytes(p12, password: passwort)
      ..usePrivateKeyBytes(p12, password: passwort);
    return HttpClient(context: ctx)
      ..connectionTimeout = const Duration(seconds: 6)
      ..idleTimeout = const Duration(seconds: 30)
      ..userAgent = 'SchnellWeg';
  }
}
