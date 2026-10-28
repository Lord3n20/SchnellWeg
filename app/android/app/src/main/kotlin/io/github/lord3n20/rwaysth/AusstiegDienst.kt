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
import android.location.Location
import android.location.LocationListener
import android.location.LocationManager
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.os.VibrationEffect
import android.os.VibratorManager

// Ausstiegs-Erinnerung (D-23): verfolgt während der Fahrt per GPS, welcher Halt zuletzt erreicht war, zeigt
// „noch N Halte“ und vibriert beim Halt vor dem Ausstieg. Ohne GPS (Tunnel, U-Bahn) meldet sie sich 2 min vor der
// Ankunftszeit. Bei Umstiegen geht es mit der nächsten Fahrt weiter. Positionen bleiben im Dienst, nichts wird
// gespeichert oder verschickt.
class AusstiegDienst : Service() {
    class Halt(val name: String, val lat: Double, val lon: Double)
    class Fahrt(val linie: String, val richtung: String, val halte: List<Halt>, val ankunft: Long)

    private var fahrten = listOf<Fahrt>()
    private var k = 0 // aktuelle Fahrt
    private var erreicht = 0 // Index des zuletzt erreichten Halts der aktuellen Fahrt (0 = Einstieg)
    private var gewarnt = false // für die aktuelle Fahrt schon vibriert
    private var hoerer: LocationListener? = null
    private val takt = Handler(Looper.getMainLooper())
    private val pruefen = object : Runnable {
        override fun run() {
            zeitPruefen()
            takt.postDelayed(this, 15_000)
        }
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == STOP) {
            stopSelf()
            return START_NOT_STICKY
        }
        fahrten = laufend ?: return START_NOT_STICKY.also { stopSelf() }
        k = 0
        erreicht = 0
        gewarnt = false
        startForeground(NR, meldung(), ServiceInfo.FOREGROUND_SERVICE_TYPE_LOCATION)
        gpsStarten()
        takt.removeCallbacks(pruefen)
        takt.post(pruefen)
        return START_NOT_STICKY
    }

    @SuppressLint("MissingPermission") // nur gestartet, wenn die App die Standort-Erlaubnis hat
    private fun gpsStarten() {
        if (hoerer != null) return
        val lm = getSystemService(LocationManager::class.java)
        val l = LocationListener { position(it) }
        hoerer = l
        for (a in listOf(LocationManager.GPS_PROVIDER, LocationManager.NETWORK_PROVIDER)) {
            if (runCatching { lm.isProviderEnabled(a) }.getOrDefault(false)) {
                lm.requestLocationUpdates(a, 4000L, 0f, mainExecutor, l)
            }
        }
    }

    private fun position(loc: Location) {
        val f = fahrten.getOrNull(k) ?: return
        if (loc.accuracy > 150) return
        val letzter = f.halte.size - 1
        val d = f.halte.map { abstand(loc, it) }
        // Halt erreicht: näher als 120 m (plus Ungenauigkeit), nur vorwärts. Weit voraus nur, wenn es zur Zeit passt
        // (eine Linie kann später erreichte Halte früh nahe streifen).
        val bald = System.currentTimeMillis() >= f.ankunft - 6 * 60_000
        for (i in letzter downTo erreicht + 1) {
            if (d[i] < 120 + loc.accuracy / 2 && (i <= erreicht + 3 || bald)) {
                erreicht = i
                break
            }
        }
        if (erreicht >= letzter) return fahrtFertig()
        val nahe = d[letzter] < 700 && (erreicht >= letzter - 3 || bald)
        if (!gewarnt && (erreicht >= letzter - 1 || nahe)) warnen()
        aktualisieren()
    }

    private fun zeitPruefen() {
        val f = fahrten.getOrNull(k) ?: return
        val jetzt = System.currentTimeMillis()
        if (!gewarnt && jetzt >= f.ankunft - 2 * 60_000) warnen()
        if (jetzt > f.ankunft + 10 * 60_000) fahrtFertig() // GPS hat den Ausstieg nicht gesehen
        if (k >= fahrten.size || jetzt > fahrten.last().ankunft + 30 * 60_000) stopSelf()
    }

    private fun warnen() {
        gewarnt = true
        val f = fahrten[k]
        getSystemService(VibratorManager::class.java).defaultVibrator
            .vibrate(VibrationEffect.createWaveform(longArrayOf(0, 700, 300, 700, 300, 700), -1))
        getSystemService(NotificationManager::class.java).notify(NR_WARNUNG, Notification.Builder(this, KANAL_WARNUNG)
            .setSmallIcon(android.R.drawable.ic_dialog_alert)
            .setContentTitle("Gleich aussteigen: ${f.halte.last().name}")
            .setContentText("Linie ${f.linie} – nächster Halt")
            .setContentIntent(oeffnen())
            .setAutoCancel(true)
            .setTimeoutAfter(10 * 60_000)
            .build())
        aktualisieren()
    }

    private fun fahrtFertig() {
        if (!gewarnt) warnen()
        k++
        erreicht = 0
        gewarnt = false
        if (k >= fahrten.size) {
            takt.postDelayed({ stopSelf() }, 60_000) // Meldung „angekommen“ kurz stehen lassen
        }
        aktualisieren()
    }

    private fun aktualisieren() = getSystemService(NotificationManager::class.java).notify(NR, meldung())

    private fun meldung(): Notification {
        val nm = getSystemService(NotificationManager::class.java)
        nm.createNotificationChannel(NotificationChannel(KANAL, "Ausstiegs-Erinnerung", NotificationManager.IMPORTANCE_LOW))
        nm.createNotificationChannel(NotificationChannel(KANAL_WARNUNG, "Jetzt aussteigen", NotificationManager.IMPORTANCE_HIGH)
            .apply { enableVibration(true) })
        val f = fahrten.getOrNull(k)
        val (titel, text) = when {
            f == null -> "Ausgestiegen" to "Erinnerung endet gleich"
            erreicht == 0 && k > 0 -> "Umsteigen: Linie ${f.linie}" to "Richtung ${f.richtung}, aussteigen ${f.halte.last().name}"
            else -> {
                val n = f.halte.size - 1 - erreicht
                "Linie ${f.linie}: noch $n ${if (n == 1) "Halt" else "Halte"}" to
                    "Aussteigen ${f.halte.last().name}" + (if (n > 1) " · nächster: ${f.halte[erreicht + 1].name}" else "")
            }
        }
        val stop = PendingIntent.getService(this, 1, Intent(this, AusstiegDienst::class.java).setAction(STOP),
            PendingIntent.FLAG_IMMUTABLE)
        return Notification.Builder(this, KANAL)
            .setSmallIcon(android.R.drawable.ic_menu_mylocation)
            .setContentTitle(titel)
            .setContentText(text)
            .setContentIntent(oeffnen())
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .addAction(Notification.Action.Builder(null, "Beenden", stop).build())
            .build()
    }

    private fun oeffnen() = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java)
        .addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP), PendingIntent.FLAG_IMMUTABLE)

    override fun onDestroy() {
        hoerer?.let { getSystemService(LocationManager::class.java).removeUpdates(it) }
        hoerer = null
        takt.removeCallbacksAndMessages(null)
        laufend = null
        super.onDestroy()
    }

    companion object {
        const val NR = 21
        const val NR_WARNUNG = 22
        const val KANAL = "aussteigen"
        const val KANAL_WARNUNG = "aussteigen_jetzt"
        const val STOP = "stop"
        @Volatile var laufend: List<Fahrt>? = null

        fun starten(c: Context, f: List<Fahrt>) {
            laufend = f
            c.startForegroundService(Intent(c, AusstiegDienst::class.java))
        }

        fun beenden(c: Context) = c.stopService(Intent(c, AusstiegDienst::class.java))

        private fun abstand(l: Location, h: Halt): Float {
            val r = FloatArray(1)
            Location.distanceBetween(l.latitude, l.longitude, h.lat, h.lon, r)
            return r[0]
        }
    }
}
