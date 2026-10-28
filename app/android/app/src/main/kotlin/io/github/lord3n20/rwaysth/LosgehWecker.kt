package io.github.lord3n20.schnellweg

import android.app.AlarmManager
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.ContentUris
import android.content.Context
import android.content.Intent
import android.provider.CalendarContract

// Losgeh-Wecker (D-29): liest Termine mit Ort aus dem Android-Kalender (bleiben auf dem Handy; die App sucht nur den
// Ortstext wie eine eingetippte Suche) und meldet sich zur berechneten Losgehzeit. Die Zeit rechnet die App, sobald
// sie geöffnet wird; hier liegt nur der Wecker (AlarmManager) und die Meldung. Vorerst lautlos (Kanal ohne Ton und
// Vibration, Betreiber 28.10.); Ton lässt sich in den Android-Einstellungen des Kanals einschalten.
object LosgehWecker {
    private const val KANAL = "losgehen"
    private const val NR = 31

    /** Termine mit Ort, die in den nächsten [stunden] beginnen; ganztägige zählen nicht. */
    fun termine(c: Context, stunden: Int): List<Map<String, Any>> {
        val jetzt = System.currentTimeMillis()
        val uri = CalendarContract.Instances.CONTENT_URI.buildUpon().also {
            ContentUris.appendId(it, jetzt)
            ContentUris.appendId(it, jetzt + stunden * 3_600_000L)
        }.build()
        val spalten = arrayOf(CalendarContract.Instances.TITLE, CalendarContract.Instances.EVENT_LOCATION,
            CalendarContract.Instances.BEGIN, CalendarContract.Instances.ALL_DAY)
        val out = mutableListOf<Map<String, Any>>()
        c.contentResolver.query(uri, spalten, null, null, "${CalendarContract.Instances.BEGIN} ASC")?.use { z ->
            while (z.moveToNext()) {
                val ort = z.getString(1)?.trim().orEmpty()
                if (ort.isEmpty() || z.getInt(3) == 1 || z.getLong(2) < jetzt) continue
                out += mapOf("titel" to (z.getString(0) ?: "Termin"), "ort" to ort, "beginn" to z.getLong(2))
            }
        }
        return out
    }

    /** Wecker auf [zeit] (ms) stellen; ersetzt einen vorherigen. Exakt, wenn das System es erlaubt, sonst ungefähr. */
    fun stellen(c: Context, zeit: Long, titel: String, text: String) {
        val am = c.getSystemService(AlarmManager::class.java)
        val pi = absicht(c, titel, text)
        if (am.canScheduleExactAlarms()) am.setExactAndAllowWhileIdle(AlarmManager.RTC_WAKEUP, zeit, pi)
        else am.setWindow(AlarmManager.RTC_WAKEUP, zeit - 60_000, 120_000, pi)
    }

    fun loeschen(c: Context) = c.getSystemService(AlarmManager::class.java).cancel(absicht(c, "", ""))

    private fun absicht(c: Context, titel: String, text: String) = PendingIntent.getBroadcast(c, 0,
        Intent(c, Melder::class.java).putExtra("titel", titel).putExtra("text", text),
        PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)

    class Melder : BroadcastReceiver() {
        override fun onReceive(c: Context, i: Intent) {
            val nm = c.getSystemService(NotificationManager::class.java)
            nm.createNotificationChannel(NotificationChannel(KANAL, "Losgeh-Wecker", NotificationManager.IMPORTANCE_DEFAULT)
                .apply { setSound(null, null); enableVibration(false) })
            val oeffnen = PendingIntent.getActivity(c, 0, Intent(c, MainActivity::class.java)
                .addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP), PendingIntent.FLAG_IMMUTABLE)
            nm.notify(NR, Notification.Builder(c, KANAL)
                .setSmallIcon(android.R.drawable.ic_lock_idle_alarm)
                .setContentTitle(i.getStringExtra("titel"))
                .setContentText(i.getStringExtra("text"))
                .setStyle(Notification.BigTextStyle().bigText(i.getStringExtra("text")))
                .setContentIntent(oeffnen)
                .setAutoCancel(true)
                .build())
        }
    }
}
