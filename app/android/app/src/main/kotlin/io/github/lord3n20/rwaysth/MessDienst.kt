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
import android.os.IBinder
import android.os.PowerManager
import java.io.BufferedWriter
import java.io.File
import java.io.FileWriter

// Messgang im Hintergrund (D-45): nimmt GPS auf, auch bei ausgeschaltetem Bildschirm oder geschlossener App, und schreibt
// jede Position sofort in eine Datei im App-Ordner. Die Seite liest diese Datei mit (lib/messung.dart); stirbt die App,
// bleibt die Messung erhalten. Zeilen: „k,<profil>“, „f,lat,lon,genau,zeit,tempo“, „p“ = Pause, „w“ = weiter.
class MessDienst : Service() {
    private var hoerer: LocationListener? = null
    private var aus: BufferedWriter? = null
    private var wach: PowerManager.WakeLock? = null
    private var titel = ""

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            STOP -> stopSelf()
            PAUSE -> if (hoerer == null) stopSelf() else pause(intent.getBooleanExtra(WERT, !pausiert))
            else -> if (intent == null || hoerer != null) {
                if (hoerer == null) stopSelf()
            } else starten(intent.getStringExtra(PROFIL) ?: "", intent.getStringExtra(TITEL) ?: "")
        }
        return START_NOT_STICKY
    }

    @SuppressLint("MissingPermission") // nur gestartet, wenn die App die Standort-Erlaubnis hat
    private fun starten(profil: String, t: String) {
        titel = t
        pausiert = false
        startForeground(NR, meldung(), ServiceInfo.FOREGROUND_SERVICE_TYPE_LOCATION)
        val f = datei(this)
        val neu = !f.exists()
        aus = BufferedWriter(FileWriter(f, true))
        if (neu) zeile("k,$profil")
        zeile("w")
        wach = getSystemService(PowerManager::class.java)
            .newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "schnellweg:messung").apply { acquire(12 * 3600_000L) }
        val l = LocationListener { loc ->
            zeile("f,${loc.latitude},${loc.longitude},${loc.accuracy},${loc.elapsedRealtimeNanos / 1e6},${if (loc.hasSpeed()) loc.speed else -1f}")
        }
        hoerer = l
        getSystemService(LocationManager::class.java).requestLocationUpdates(LocationManager.GPS_PROVIDER, 1000L, 0f, mainExecutor, l)
        laeuft = true
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
            .setContentText("Zum Abschließen in der App unter Profile → Messen")
            .setContentIntent(offen)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .addAction(aktion(if (pausiert) "Weiter" else "Pause", 2, Intent(this, klasse).setAction(PAUSE).putExtra(WERT, !pausiert)))
            .addAction(aktion("Anhalten", 3, Intent(this, klasse).setAction(STOP)))
            .build()
    }

    override fun onDestroy() {
        hoerer?.let { getSystemService(LocationManager::class.java).removeUpdates(it) }
        hoerer = null
        runCatching { aus?.close() }
        aus = null
        wach?.takeIf { it.isHeld }?.release()
        wach = null
        laeuft = false
        pausiert = false
        super.onDestroy()
    }

    companion object {
        const val NR = 23
        const val KANAL = "messung"
        const val STOP = "stop"
        const val PAUSE = "pause"
        const val WERT = "wert"
        const val PROFIL = "profil"
        const val TITEL = "titel"
        @Volatile var laeuft = false
        @Volatile var pausiert = false

        fun datei(c: Context) = File(c.filesDir, "messung.txt")

        fun starten(c: Context, profil: String, titel: String) {
            laeuft = true // die Seite fragt sofort nach; der Dienst startet erst gleich
            c.startForegroundService(Intent(c, MessDienst::class.java).putExtra(PROFIL, profil).putExtra(TITEL, titel))
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
