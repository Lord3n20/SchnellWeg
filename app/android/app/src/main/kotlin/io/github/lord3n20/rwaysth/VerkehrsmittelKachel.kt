package io.github.lord3n20.schnellweg

import android.app.AlertDialog
import android.graphics.drawable.Icon
import android.service.quicksettings.Tile
import android.service.quicksettings.TileService
import org.json.JSONObject
import java.io.File

// Schnelleinstellung „Verkehrsmittel“: zeigt das aktive Profil, Antippen öffnet die Auswahl aller Profile.
// Schreibt nur `aktiv` in tempo.json (dieselbe Datei wie lib/tempo.dart); die App liest sie beim Zurückkommen neu
// und rechnet die aktuelle Verbindung damit neu.
class VerkehrsmittelKachel : TileService() {
    private val datei get() = File(filesDir, "tempo.json")

    // null: Datei da, aber nicht lesbar (dann nichts schreiben, die App legt sie beiseite)
    private fun lesenOderNull(): JSONObject? = try {
        if (datei.exists()) JSONObject(datei.readText()) else JSONObject()
    } catch (e: Exception) {
        null
    }

    private fun lesen(): JSONObject = lesenOderNull() ?: JSONObject() // die App nimmt dann ihre Vorgaben

    private fun aktiv(): String = lesen().optString("aktiv", "entspannt")

    // Schlüssel → Anzeigename wie in lib/tempo.dart; ohne Datei gibt es nur die Vorgaben
    private fun profile(): List<Pair<String, String>> {
        val p = lesen().optJSONObject("profile")
        if (p == null || p.length() == 0) return listOf("entspannt" to "Zu Fuß", "Auto" to "Auto")
        return p.keys().asSequence().map { it to anzeige(it, p.optJSONObject(it)) }.toList()
    }

    private fun anzeige(profil: String, j: JSONObject?): String = j?.optString("titel", "").orEmpty()
        .ifEmpty { mapOf("entspannt" to "Zu Fuß", "Einrad" to "Custom 1", "Einrad schnell" to "Custom 2")[profil] ?: profil }

    // Art aus tempo.json; ohne Eintrag (Stand vor D-34, die App stellt ihn beim nächsten Öffnen um) nach dem Schlüssel
    private fun artVon(profil: String): String =
        lesen().optJSONObject("profile")?.optJSONObject(profil)?.optString("art", "").orEmpty().ifEmpty {
            when (profil) {
                "entspannt", "normal", "schnell" -> "fuss"
                "Auto" -> "auto"
                else -> "eigen"
            }
        }

    override fun onStartListening() = zeigen()

    private fun zeigen() {
        val t = qsTile ?: return
        val profil = aktiv()
        t.label = "Verkehrsmittel"
        t.subtitle = anzeige(profil, lesen().optJSONObject("profile")?.optJSONObject(profil)).replaceFirstChar { it.uppercase() }
        t.icon = Icon.createWithResource(this, when (artVon(profil)) {
            "fuss" -> R.drawable.kachel_fuss
            "auto" -> R.drawable.kachel_auto
            else -> if (lesen().optJSONObject("profile")?.optJSONObject(profil)?.optJSONObject("meiden")?.optBoolean("fusswege") == true)
                R.drawable.kachel_rad else R.drawable.kachel_eigen
        })
        t.state = Tile.STATE_ACTIVE
        t.updateTile()
    }

    override fun onClick() {
        val jetzt = aktiv()
        val liste = profile()
        val namen = liste.map { (k, name) -> if (k == jetzt) "$name  ✓" else name }.toTypedArray()
        val d = AlertDialog.Builder(this, android.R.style.Theme_DeviceDefault_Dialog_Alert)
            .setTitle("Verkehrsmittel")
            .setItems(namen) { _, i -> waehlen(liste[i].first) }
            .setNegativeButton("Abbrechen", null)
            .create()
        showDialog(d)
    }

    private fun waehlen(profil: String) {
        val j = lesenOderNull() ?: return zeigen()
        j.put("aktiv", profil)
        if (!j.has("profile")) { // noch nie gespeichert: kein alter Stand, den die App umstellen müsste
            j.put("v", 2)
            j.put("profile", JSONObject())
        }
        val zw = File(filesDir, "tempo.json.kachel.neu") // rename ist atomar, wie in lib/tempo.dart
        zw.writeText(j.toString())
        zw.renameTo(datei)
        zeigen()
    }
}
