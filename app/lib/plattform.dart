// iPhone (D-53): Was dort (noch) fehlt, blendet die App aus, statt Knöpfe ohne Wirkung zu zeigen. Ohne bezahlten
// Apple-Account gibt es kein Selbstupdate (SideStore/AltStore aktualisiert), und die Hintergrunddienste von Android
// (Messgang, Aufzeichnung, Ausstiegs-Erinnerung, Losgeh-Wecker) haben noch kein Gegenstück.
import 'package:flutter/foundation.dart';

/// Läuft die App auf einem iPhone? Über [defaultTargetPlatform], damit Tests es umstellen können.
bool get aufIos => defaultTargetPlatform == TargetPlatform.iOS;
