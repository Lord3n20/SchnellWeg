package io.github.lord3n20.schnellweg

import android.annotation.SuppressLint
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.location.LocationListener
import android.location.LocationManager
import android.location.Location
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.os.PowerManager
import android.os.SystemClock
import io.flutter.FlutterInjector
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.embedding.engine.dart.DartExecutor
import io.flutter.plugin.common.MethodChannel
import java.io.BufferedWriter
import java.io.File
import java.io.FileWriter

// Messgang im Hintergrund (D-45): nimmt GPS auf, auch bei ausgeschaltetem Bildschirm oder geschlossener App, und schreibt
// jede Position sofort in eine Datei im App-Ordner. Die Seite liest diese Datei mit (lib/messung.dart); stirbt die App,
// bleibt die Messung erhalten. Zeilen: „k,<profil>“ (Kalibrieren) oder „l,<profil>“ + „r,<json>“ (Route, D-46), „t,uhr,zeit“,
// „f,lat,lon,genau,zeit,tempo“, „p“ = Pause, „w“ = weiter. Beenden wertet ohne Oberfläche aus (Dart messungAbschliessen).
class MessDienst : Service() {
    private var hoerer: LocationListener? = null
    private var aus: BufferedWriter? = null
    private var wach: PowerManager.WakeLock? = null
    private var titel = ""
    private var ziel: Location? = null
    private var amZiel = 0
    private var wertet = false

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            STOP -> stopSelf()
            ENDE -> beenden()
            PAUSE -> if (hoerer == null) stopSelf() else pause(intent.getBooleanExtra(WERT, !pausiert))
            else -> if (intent == null || hoerer != null) {
                if (hoerer == null) stopSelf()
            } else starten(intent)
        }
        return START_NOT_STICKY
    }

    @SuppressLint("MissingPermission") // nur gestartet, wenn die App die Standort-Erlaubnis hat
    private fun starten(i: Intent) {
        val profil = i.getStringExtra(PROFIL) ?: ""
        val route = i.getStringExtra(ROUTE)
        titel = i.getStringExtra(TITEL) ?: ""
        ziel = if (i.hasExtra(LAT)) Location("ziel").apply {
            latitude = i.getDoubleExtra(LAT, 0.0)
            longitude = i.getDoubleExtra(LON, 0.0)
        } else null
        pausiert = false
        startForeground(NR, meldung(), ServiceInfo.FOREGROUND_SERVICE_TYPE_LOCATION)
        val f = datei(this)
        val neu = !f.exists()
        aus = BufferedWriter(FileWriter(f, true))
        if (neu) {
            zeile(if (route != null) "l,$profil" else "k,$profil")
            if (route != null) zeile("r,$route")
        }
        zeile("t,${System.currentTimeMillis()},${SystemClock.elapsedRealtime()}")
        zeile("w")
        wach = getSystemService(PowerManager::class.java)
            .newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "schnellweg:messung").apply { acquire(12 * 3600_000L) }
        val l = LocationListener { loc ->
            zeile("f,${loc.latitude},${loc.longitude},${loc.accuracy},${loc.elapsedRealtimeNanos / 1e6},${if (loc.hasSpeed()) loc.speed else -1f}")
            zielPruefen(loc)
        }
        hoerer = l
        getSystemService(LocationManager::class.java).requestLocationUpdates(LocationManager.GPS_PROVIDER, 1000L, 0f, mainExecutor, l)
        laeuft = true
    }

    private fun zielPruefen(loc: Location) {
        val z = ziel ?: return
        amZiel = if (loc.accuracy <= 30 && loc.distanceTo(z) < 40) amZiel + 1 else 0
        if (amZiel >= 5) beenden() // angekommen
    }

    private fun gpsAus() {
        hoerer?.let { getSystemService(LocationManager::class.java).removeUpdates(it) }
        hoerer = null
        runCatching { aus?.close() }
        aus = null
        laeuft = false
    }

    // Wertet in einer Flutter-Engine ohne Oberfläche aus; der Dienst bleibt so lange im Vordergrund.
    private fun beenden() {
        if (wertet) return
        wertet = true
        gpsAus()
        getSystemService(NotificationManager::class.java).notify(NR, Notification.Builder(this, KANAL)
            .setSmallIcon(android.R.drawable.ic_menu_mylocation)
            .setContentTitle("Messung wird ausgewertet")
            .setOngoing(true)
            .build())
        val haupt = Handler(Looper.getMainLooper())
        val loader = FlutterInjector.instance().flutterLoader()
        loader.startInitialization(applicationContext)
        loader.ensureInitializationCompleteAsync(applicationContext, null, haupt) {
            val engine = FlutterEngine(applicationContext)
            var aus = false
            val ende = { t: String, x: String ->
                if (!aus) {
                    aus = true
                    engine.destroy()
                    ergebnis(t, x)
                    stopSelf()
                }
            }
            haupt.postDelayed({ ende("Messung beendet", "In der App unter Profile → Messen abschließen") }, 60_000)
            MethodChannel(engine.dartExecutor.binaryMessenger, "schnellweg/messung/hinter").setMethodCallHandler { call, result ->
                result.success(null)
                haupt.post { ende(call.argument<String>("titel") ?: "Messung beendet", call.argument<String>("text") ?: "") }
            }
            engine.dartExecutor.executeDartEntrypoint(DartExecutor.DartEntrypoint(loader.findAppBundlePath(), "messungAbschliessen"))
        }
    }

    private fun ergebnis(t: String, x: String) {
        val offen = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java)
            .addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP), PendingIntent.FLAG_IMMUTABLE)
        getSystemService(NotificationManager::class.java).notify(NR_ERGEBNIS, Notification.Builder(this, KANAL)
            .setSmallIcon(android.R.drawable.ic_menu_mylocation)
            .setContentTitle(t)
            .setContentText(x)
            .setStyle(Notification.BigTextStyle().bigText(x))
            .setContentIntent(offen)
            .setAutoCancel(true)
            .build())
    }

    private fun pause(wert: Boolean) {
        if (wert == pausiert) return
        pausiert = wert
        zeile(if (wert) "p" else "w")
        getSystemService(NotificationManager::class.java).notify(NR, meldung())
    }

    private fun zeile(z: String) {
        runCatching {
            aus?.write(z)
            aus?.newLine()
            aus?.flush()
        }
    }

    private fun aktion(text: String, code: Int, i: Intent) =
        Notification.Action.Builder(null, text, PendingIntent.getService(this, code, i, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)).build()

    private fun meldung(): Notification {
        getSystemService(NotificationManager::class.java)
            .createNotificationChannel(NotificationChannel(KANAL, "Messung", NotificationManager.IMPORTANCE_LOW))
        val offen = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java)
            .addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP), PendingIntent.FLAG_IMMUTABLE)
        val klasse = MessDienst::class.java
        return Notification.Builder(this, KANAL)
            .setSmallIcon(android.R.drawable.ic_menu_mylocation)
            .setContentTitle(if (pausiert) "Messung pausiert: $titel" else "Messung läuft: $titel")
            .setContentText(if (ziel != null) "Endet am Ziel von selbst" else "Beenden wertet die Messung aus")
            .setContentIntent(offen)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .addAction(aktion(if (pausiert) "Weiter" else "Pause", 2, Intent(this, klasse).setAction(PAUSE).putExtra(WERT, !pausiert)))
            .addAction(aktion("Beenden", 3, Intent(this, klasse).setAction(ENDE)))
            .build()
    }

    override fun onDestroy() {
        gpsAus()
        wach?.takeIf { it.isHeld }?.release()
        wach = null
        pausiert = false
        super.onDestroy()
    }

    companion object {
        const val NR = 23
        const val NR_ERGEBNIS = 24
        const val ENDE = "ende"
        const val ROUTE = "route"
        const val LAT = "lat"
        const val LON = "lon"
        const val KANAL = "messung"
        const val STOP = "stop"
        const val PAUSE = "pause"
        const val WERT = "wert"
        const val PROFIL = "profil"
        const val TITEL = "titel"
        @Volatile var laeuft = false
        @Volatile var pausiert = false

        fun datei(c: Context) = File(c.filesDir, "messung.txt")

        fun starten(c: Context, profil: String, titel: String, route: String?, lat: Double?, lon: Double?) {
            laeuft = true // die Seite fragt sofort nach; der Dienst startet erst gleich
            c.startForegroundService(Intent(c, MessDienst::class.java).putExtra(PROFIL, profil).putExtra(TITEL, titel).apply {
                if (route != null) putExtra(ROUTE, route)
                if (lat != null && lon != null) putExtra(LAT, lat).putExtra(LON, lon)
            })
        }

        fun abschliessen(c: Context) {
            c.startService(Intent(c, MessDienst::class.java).setAction(ENDE))
        }

        fun pausieren(c: Context, wert: Boolean) {
            c.startService(Intent(c, MessDienst::class.java).setAction(PAUSE).putExtra(WERT, wert))
        }

        fun beenden(c: Context): Boolean {
            laeuft = false
            return c.stopService(Intent(c, MessDienst::class.java))
        }
    }
}
