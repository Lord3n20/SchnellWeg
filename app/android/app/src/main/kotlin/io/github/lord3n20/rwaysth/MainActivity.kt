package io.github.lord3n20.schnellweg

import android.Manifest
import android.content.pm.PackageManager
import android.location.Location
import android.location.LocationManager
import android.os.CancellationSignal
import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel

// Standort über den LocationManager des Systems (keine Play-Dienste, siehe lib/standort.dart).
// Eine einzelne Position auf Anfrage; kein Abo, kein Hintergrund.
class MainActivity : FlutterActivity() {
    private val wartend = mutableListOf<MethodChannel.Result>()

    override fun configureFlutterEngine(engine: FlutterEngine) {
        super.configureFlutterEngine(engine)
        MethodChannel(engine.dartExecutor.binaryMessenger, "schnellweg/standort").setMethodCallHandler { call, result ->
            if (call.method != "holen") return@setMethodCallHandler result.notImplemented()
            if (erlaubt()) holen(result) else {
                wartend += result
                if (wartend.size == 1) requestPermissions(
                    arrayOf(Manifest.permission.ACCESS_FINE_LOCATION, Manifest.permission.ACCESS_COARSE_LOCATION), ANFRAGE)
            }
        }
    }

    override fun onRequestPermissionsResult(code: Int, perms: Array<out String>, res: IntArray) {
        super.onRequestPermissionsResult(code, perms, res)
        if (code != ANFRAGE) return
        val alle = wartend.toList().also { wartend.clear() }
        for (r in alle) if (erlaubt()) holen(r) else r.success(null)
    }

    private fun erlaubt() = checkSelfPermission(Manifest.permission.ACCESS_FINE_LOCATION) == PackageManager.PERMISSION_GRANTED ||
        checkSelfPermission(Manifest.permission.ACCESS_COARSE_LOCATION) == PackageManager.PERMISSION_GRANTED

    @Suppress("MissingPermission")
    private fun holen(result: MethodChannel.Result) {
        val lm = getSystemService(LocationManager::class.java)
        val anbieter = listOf(LocationManager.FUSED_PROVIDER, LocationManager.GPS_PROVIDER, LocationManager.NETWORK_PROVIDER)
            .filter { runCatching { lm.isProviderEnabled(it) }.getOrDefault(false) }
        // Frische letzte Position (< 60 s) reicht für „Halte in der Nähe“ und spart den GPS-Start.
        val jetzt = SystemClock.elapsedRealtimeNanos()
        anbieter.mapNotNull { runCatching { lm.getLastKnownLocation(it) }.getOrNull() }
            .filter { jetzt - it.elapsedRealtimeNanos < 60_000_000_000L }
            .minByOrNull { it.accuracy }
            ?.let { return result.success(karte(it)) }
        if (anbieter.isEmpty()) return result.success(null)

        val haupt = Handler(Looper.getMainLooper())
        val abbruch = CancellationSignal()
        var fertig = false
        val ende = { loc: Location? ->
            if (!fertig) {
                fertig = true
                abbruch.cancel()
                result.success(loc?.let(::karte))
            }
        }
        haupt.postDelayed({ ende(null) }, 10_000)
        // Erste brauchbare Antwort gewinnt (Netz meist schnell, GPS genauer).
        for (a in anbieter) lm.getCurrentLocation(a, abbruch, mainExecutor) { loc -> if (loc != null) ende(loc) }
    }

    private fun karte(l: Location) = mapOf("lat" to l.latitude, "lon" to l.longitude, "genau" to l.accuracy.toDouble())

    companion object { const val ANFRAGE = 7 }
}
