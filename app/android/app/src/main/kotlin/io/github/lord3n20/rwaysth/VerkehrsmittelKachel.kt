package io.github.lord3n20.schnellweg

import android.app.AlertDialog
import android.graphics.drawable.Icon
import android.service.quicksettings.Tile
import android.service.quicksettings.TileService
import org.json.JSONObject
import java.io.File

// Schnelleinstellung „Verkehrsmittel“: zeigt das aktive Tempo-Profil, Antippen öffnet die Auswahl Fuß/Custom/Räder.
// Schreibt nur `aktiv` in tempo.json (dieselbe Datei wie lib/tempo.dart); die App liest sie beim Zurückkommen neu
// und rechnet die aktuelle Verbindung damit neu.
class VerkehrsmittelKachel : TileService() {
    // Art → Profil, das beim Wechsel auf diese Art aktiv wird (Namen wie _vorgaben in lib/tempo.dart)
    private val arten = listOf(
        Triple("fuss", "Zu Fuß", "entspannt"),
        Triple("einrad", "Custom", "Einrad"), // Profilschlüssel bleibt „Einrad“, Anzeigename steht als `titel` in tempo.json
        Triple("klapprad", "Klapprad", "Klapprad"),
        Triple("fixie", "Fixie", "Fixie"),
        Triple("rennrad", "Rennrad", "Rennrad"),
        Triple("mtb", "Mountainbike", "Mountainbike"),
    )

    private val datei get() = File(filesDir, "tempo.json")

    private fun lesen(): JSONObject = try {
        JSONObject(datei.readText())
    } catch (e: Exception) {
        JSONObject() // noch nie gespeichert oder kaputt: die App nimmt dann ihre Vorgaben
    }

    private fun aktiv(): String = lesen().optString("aktiv", "entspannt")

    private fun artVon(profil: String): String =
        arten.firstOrNull { (_, _, p) -> p == profil }?.first
            ?: (if (profil.startsWith("Einrad")) "einrad" else null)
            ?: arten.firstOrNull { (_, name, _) -> profil.startsWith(name) }?.first
            ?: "fuss" // entspannt / normal / schnell

    override fun onStartListening() = zeigen()

    private fun zeigen() {
        val t = qsTile ?: return
        val profil = aktiv()
        t.label = "Verkehrsmittel"
        val titel = lesen().optJSONObject("profile")?.optJSONObject(profil)?.optString("titel", "").orEmpty()
        t.subtitle = titel.ifEmpty { mapOf("Einrad" to "Custom 1", "Einrad schnell" to "Custom 2")[profil] ?: profil }
            .replaceFirstChar { it.uppercase() }
        t.icon = Icon.createWithResource(this, if (artVon(profil) == "fuss") R.drawable.kachel_fuss else R.drawable.kachel_rad)
        t.state = Tile.STATE_ACTIVE
        t.updateTile()
    }

    override fun onClick() {
        val jetzt = artVon(aktiv())
        val namen = arten.map { (art, name, _) -> if (art == jetzt) "$name  ✓" else name }.toTypedArray()
        val d = AlertDialog.Builder(this, android.R.style.Theme_DeviceDefault_Dialog_Alert)
            .setTitle("Verkehrsmittel")
            .setItems(namen) { _, i -> waehlen(arten[i].first, arten[i].third) }
            .setNegativeButton("Abbrechen", null)
            .create()
        showDialog(d)
    }

    private fun waehlen(art: String, profil: String) {
        val j = lesen()
        if (artVon(j.optString("aktiv", "entspannt")) != art) { // gleiche Art: eigenes Profil (z. B. „schnell“) behalten
            j.put("aktiv", profil)
            if (!j.has("profile")) j.put("profile", JSONObject())
            datei.writeText(j.toString())
        }
        zeigen()
    }
}
