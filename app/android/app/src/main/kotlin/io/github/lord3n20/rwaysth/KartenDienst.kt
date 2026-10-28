package io.github.lord3n20.schnellweg

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.wifi.WifiManager
import android.os.IBinder
import android.os.PowerManager

// Vordergrunddienst, solange eine Offline-Karte lädt (D-20): hält den Prozess, die CPU und das WLAN wach, damit der
// Download (in Dart, lib/kartenlager.dart) auch bei ausgeschaltetem Bildschirm oder anderer App weiterläuft.
// Zeigt den Fortschritt als Benachrichtigung; lädt selbst nichts.
class KartenDienst : Service() {
    private var wach: PowerManager.WakeLock? = null
    private var wlan: WifiManager.WifiLock? = null

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val text = intent?.getStringExtra("text") ?: "Karte wird geladen"
        val p = intent?.getIntExtra("prozent", -1) ?: -1
        startForeground(NR, meldung(text, p), ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC)
        if (wach == null) {
            wach = getSystemService(PowerManager::class.java)
                .newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "schnellweg:karte").apply { acquire(6 * 3600_000L) }
            @Suppress("DEPRECATION") // HIGH_PERF: einziger Modus, der auch mit Bildschirm aus volle Rate hält
            wlan = getSystemService(WifiManager::class.java)
                .createWifiLock(WifiManager.WIFI_MODE_FULL_HIGH_PERF, "schnellweg:karte").apply { acquire() }
        }
        return START_NOT_STICKY // ohne die App gibt es nichts weiterzuladen
    }

    // Wischt man die App weg, endet auch der Download; die .part-Datei wird beim nächsten Start fortgesetzt.
    override fun onTaskRemoved(rootIntent: Intent?) = stopSelf()

    override fun onDestroy() {
        wach?.takeIf { it.isHeld }?.release()
        wlan?.takeIf { it.isHeld }?.release()
        super.onDestroy()
    }

    private fun meldung(text: String, prozent: Int): Notification {
        val nm = getSystemService(NotificationManager::class.java)
        nm.createNotificationChannel(NotificationChannel(KANAL, "Kartendownload", NotificationManager.IMPORTANCE_LOW))
        val oeffnen = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java)
            .addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP), PendingIntent.FLAG_IMMUTABLE)
        return Notification.Builder(this, KANAL)
            .setSmallIcon(android.R.drawable.stat_sys_download)
            .setContentTitle(text)
            .setContentIntent(oeffnen)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setProgress(100, prozent.coerceAtLeast(0), prozent < 0)
            .build()
    }

    companion object {
        const val NR = 20
        const val KANAL = "karte"

        fun melden(c: Context, text: String, prozent: Int) {
            c.startForegroundService(Intent(c, KartenDienst::class.java).putExtra("text", text).putExtra("prozent", prozent))
        }

        fun beenden(c: Context) = c.stopService(Intent(c, KartenDienst::class.java))
    }
}
