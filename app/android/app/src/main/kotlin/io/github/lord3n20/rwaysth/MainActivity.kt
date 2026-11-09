package io.github.lord3n20.schnellweg

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.content.pm.ShortcutInfo
import android.content.pm.ShortcutManager
import android.graphics.drawable.Icon
import android.location.Location
import android.location.LocationManager
import android.os.CancellationSignal
import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import android.location.LocationListener
import android.view.WindowManager
import io.flutter.plugin.common.EventChannel
import io.flutter.plugin.common.MethodChannel

// Standort über den LocationManager des Systems (keine Play-Dienste, siehe lib/standort.dart).
// Eine einzelne Position auf Anfrage; laufende GPS-Positionen, solange eine Route offen ist. Kanal schnellweg/messung
// steuert den MessDienst (Messgang im Hintergrund, D-45), schnellweg/karte den KartenDienst (Download im Hintergrund),
// schnellweg/aussteigen den AusstiegDienst (Ausstiegs-Erinnerung), schnellweg/sperrbildschirm, ob die Activity über dem
// Sperrbildschirm erscheinen darf (nur solange die Detailansicht offen ist), schnellweg/losgehen den Losgeh-Wecker,
// schnellweg/update das Selbstupdate, schnellweg/kurzbefehl die Favoriten am App-Icon, schnellweg/teilen die Android-Auswahl „Teilen“.
class MainActivity : FlutterActivity() {
    private val wartend = mutableListOf<MethodChannel.Result>()
    private var laufend: LocationListener? = null
    private var gefragt = false
    private val kalenderWartend = mutableListOf<MethodChannel.Result>()
    private var kurzKanal: MethodChannel? = null
    private var kurzOffen: Map<String, Any>? = null

    override fun configureFlutterEngine(engine: FlutterEngine) {
        super.configureFlutterEngine(engine)
        EventChannel(engine.dartExecutor.binaryMessenger, "schnellweg/standort/laufend").setStreamHandler(
            object : EventChannel.StreamHandler {
                @Suppress("MissingPermission")
                override fun onListen(args: Any?, sink: EventChannel.EventSink) {
                    val lm = getSystemService(LocationManager::class.java)
                    if (!erlaubt()) return sink.error("erlaubnis", null, null)
                    if (!runCatching { lm.isProviderEnabled(LocationManager.GPS_PROVIDER) }.getOrDefault(false)) {
                        return sink.error("aus", null, null)
                    }
                    val l = LocationListener { loc ->
                        sink.success(karte(loc) + mapOf(
                            "zeit" to loc.elapsedRealtimeNanos / 1e6,
                            "tempo" to (if (loc.hasSpeed()) loc.speed.toDouble() else -1.0),
                        ))
                    }
                    laufend = l
                    lm.requestLocationUpdates(LocationManager.GPS_PROVIDER, 1000L, 0f, mainExecutor, l)
                    if (args != false) window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
                }

                override fun onCancel(args: Any?) = laufendBeenden()
            })
        MethodChannel(engine.dartExecutor.binaryMessenger, "schnellweg/messung").setMethodCallHandler { call, result ->
            when (call.method) {
                "starten" -> {
                    if (!erlaubt()) return@setMethodCallHandler result.error("erlaubnis", null, null)
                    if (!runCatching { getSystemService(LocationManager::class.java).isProviderEnabled(LocationManager.GPS_PROVIDER) }.getOrDefault(false)) {
                        return@setMethodCallHandler result.error("aus", null, null)
                    }
                    meldungenErbitten()
                    MessDienst.starten(this, call.argument<String>("profil")!!, call.argument<String>("titel")!!,
                        call.argument<String>("route"), call.argument<Double>("lat"), call.argument<Double>("lon"))
                    result.success(null)
                }
                "pause" -> {
                    MessDienst.pausieren(this, call.arguments as Boolean)
                    result.success(null)
                }
                "beenden" -> result.success(MessDienst.beenden(this))
                "abschliessen" -> {
                    MessDienst.abschliessen(this)
                    result.success(null)
                }
                "laeuft" -> result.success(MessDienst.laeuft)
                else -> result.notImplemented()
            }
        }
        MethodChannel(engine.dartExecutor.binaryMessenger, "schnellweg/karte").setMethodCallHandler { call, result ->
            when (call.method) {
                "melden" -> {
                    meldungenErbitten()
                    KartenDienst.melden(this, call.argument<String>("text")!!, call.argument<Int>("prozent")!!)
                    result.success(null)
                }
                "beenden" -> result.success(KartenDienst.beenden(this))
                else -> result.notImplemented()
            }
        }
        MethodChannel(engine.dartExecutor.binaryMessenger, "schnellweg/aussteigen").setMethodCallHandler { call, result ->
            when (call.method) {
                "starten" -> {
                    if (!erlaubt()) return@setMethodCallHandler result.error("erlaubnis", null, null)
                    meldungenErbitten()
                    val fahrten = call.argument<List<Map<String, Any>>>("fahrten")!!.map { f ->
                        @Suppress("UNCHECKED_CAST")
                        AusstiegDienst.Fahrt(f["linie"] as String, f["richtung"] as String,
                            (f["halte"] as List<Map<String, Any>>).map {
                                AusstiegDienst.Halt(it["name"] as String, it["lat"] as Double, it["lon"] as Double)
                            },
                            (f["ankunft"] as Number).toLong())
                    }
                    AusstiegDienst.starten(this, fahrten)
                    result.success(null)
                }
                "beenden" -> result.success(AusstiegDienst.beenden(this))
                "laeuft" -> result.success(AusstiegDienst.laufend != null)
                else -> result.notImplemented()
            }
        }
        MethodChannel(engine.dartExecutor.binaryMessenger, "schnellweg/sperrbildschirm").setMethodCallHandler { call, result ->
            if (call.method != "zeigen") return@setMethodCallHandler result.notImplemented()
            setShowWhenLocked(call.arguments as Boolean)
            result.success(null)
        }
        MethodChannel(engine.dartExecutor.binaryMessenger, "schnellweg/losgehen").setMethodCallHandler { call, result ->
            when (call.method) {
                "erlaubt" -> result.success(kalenderErlaubt())
                "erbitten" -> if (kalenderErlaubt()) result.success(true) else {
                    kalenderWartend += result
                    if (kalenderWartend.size == 1) requestPermissions(arrayOf(Manifest.permission.READ_CALENDAR), ANFRAGE + 2)
                }
                "termine" -> result.success(if (kalenderErlaubt()) LosgehWecker.termine(this, call.argument<Int>("stunden")!!) else null)
                "stellen" -> {
                    meldungenErbitten()
                    LosgehWecker.kanal(this, call, result)
                }
                else -> LosgehWecker.kanal(this, call, result)
            }
        }
        MethodChannel(engine.dartExecutor.binaryMessenger, "schnellweg/teilen").setMethodCallHandler { call, result ->
            if (call.method != "teilen") return@setMethodCallHandler result.notImplemented()
            startActivity(Intent.createChooser(
                Intent(Intent.ACTION_SEND).setType("text/plain").putExtra(Intent.EXTRA_TEXT, call.arguments as String), null))
            result.success(null)
        }
        kurzLesen(intent)
        kurzKanal = MethodChannel(engine.dartExecutor.binaryMessenger, "schnellweg/kurzbefehl").apply {
            setMethodCallHandler { call, result ->
                when (call.method) {
                    "setzen" -> {
                        @Suppress("UNCHECKED_CAST")
                        kurzSetzen(call.arguments as List<Map<String, Any>>)
                        result.success(null)
                    }
                    "abholen" -> {
                        result.success(kurzOffen)
                        kurzOffen = null
                    }
                    else -> result.notImplemented()
                }
            }
        }
        MethodChannel(engine.dartExecutor.binaryMessenger, "schnellweg/update").setMethodCallHandler { call, result ->
            Selbstupdate.kanal(this, call, result)
        }
        MethodChannel(engine.dartExecutor.binaryMessenger, "schnellweg/standort").setMethodCallHandler { call, result ->
            if (call.method != "holen") return@setMethodCallHandler result.notImplemented()
            if (erlaubt()) holen(result) else {
                wartend += result
                if (wartend.size == 1) requestPermissions(
                    arrayOf(Manifest.permission.ACCESS_FINE_LOCATION, Manifest.permission.ACCESS_COARSE_LOCATION), ANFRAGE)
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        if (kurzLesen(intent)) kurzKanal?.invokeMethod("neu", null)
    }

    // Aus dem Verlauf (Letzte Apps) zählt der Kurzbefehl nicht noch einmal.
    private fun kurzLesen(i: Intent?): Boolean {
        if (i?.action != KURZBEFEHL || i.flags and Intent.FLAG_ACTIVITY_LAUNCHED_FROM_HISTORY != 0) return false
        kurzOffen = mapOf(
            "art" to (i.getStringExtra("art") ?: "karte"), "name" to (i.getStringExtra("name") ?: ""),
            "detail" to (i.getStringExtra("detail") ?: ""),
            "lat" to i.getDoubleExtra("lat", 0.0), "lon" to i.getDoubleExtra("lon", 0.0))
        i.action = Intent.ACTION_MAIN
        return true
    }

    // Die Kennung hängt am Ort: Ein aufs Homescreen gezogener Kurzbefehl zeigt auch nach dem Umsortieren denselben Favoriten.
    // Eigener Thread: Der Aufruf geht ans System und läuft bei jedem App-Start, die Oberfläche soll nicht darauf warten.
    private fun kurzSetzen(orte: List<Map<String, Any>>) = Thread {
        runCatching {
            val m = getSystemService(ShortcutManager::class.java)
            m.setDynamicShortcuts(orte.take(m.maxShortcutCountPerActivity).map { o ->
                ShortcutInfo.Builder(this, "ziel_${o["lat"]}_${o["lon"]}")
                    .setShortLabel(o["name"] as String)
                    .setIcon(Icon.createWithResource(this, R.drawable.kurz_stern))
                    .setIntent(Intent(this, MainActivity::class.java).setAction(KURZBEFEHL)
                        .putExtra("art", o["art"] as String).putExtra("name", o["name"] as String)
                        .putExtra("detail", o["detail"] as String)
                        .putExtra("lat", o["lat"] as Double).putExtra("lon", o["lon"] as Double))
                    .build()
            })
        }
    }.start()

    override fun onRequestPermissionsResult(code: Int, perms: Array<out String>, res: IntArray) {
        super.onRequestPermissionsResult(code, perms, res)
        if (code == ANFRAGE + 2) {
            val alle = kalenderWartend.toList().also { kalenderWartend.clear() }
            for (r in alle) r.success(kalenderErlaubt())
        }
        if (code != ANFRAGE) return
        val alle = wartend.toList().also { wartend.clear() }
        for (r in alle) if (erlaubt()) holen(r) else r.success(null)
    }

    // Beim ersten Bedarf einmal um Benachrichtigungen bitten; ohne laufen die Dienste trotzdem (Meldung unsichtbar).
    private fun meldungenErbitten() {
        if (checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED && !gefragt) {
            gefragt = true
            requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS), ANFRAGE + 1)
        }
    }

    private fun kalenderErlaubt() = checkSelfPermission(Manifest.permission.READ_CALENDAR) == PackageManager.PERMISSION_GRANTED

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

    private fun laufendBeenden() {
        laufend?.let { getSystemService(LocationManager::class.java).removeUpdates(it) }
        laufend = null
        window.clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
    }

    override fun onDestroy() {
        laufendBeenden()
        super.onDestroy()
    }

    private fun karte(l: Location) = mapOf("lat" to l.latitude, "lon" to l.longitude, "genau" to l.accuracy.toDouble())

    companion object {
        const val ANFRAGE = 7
        const val KURZBEFEHL = "io.github.lord3n20.schnellweg.KURZBEFEHL"
    }
}
