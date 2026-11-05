package io.github.lord3n20.schnellweg

import android.app.Activity
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.pm.PackageInstaller
import android.net.Uri
import android.provider.Settings
import android.widget.Toast
import io.flutter.plugin.common.MethodCall
import io.flutter.plugin.common.MethodChannel
import java.io.File
import java.security.MessageDigest

// Selbstupdate (D-42): prüft die geladene APK und übergibt sie dem PackageInstaller des Systems. Android zeigt die
// Bestätigung und prüft selbst die Signatur; eine fremd signierte APK scheitert, die App und ihre Daten bleiben.
object Selbstupdate {
    private var wartend: MethodChannel.Result? = null

    fun kanal(a: Activity, call: MethodCall, result: MethodChannel.Result) {
        when (call.method) {
            "stand" -> {
                val i = a.packageManager.getPackageInfo(a.packageName, 0)
                result.success(mapOf("version" to i.versionName, "code" to i.longVersionCode))
            }
            "erlaubt" -> result.success(a.packageManager.canRequestPackageInstalls())
            "erlaubnis" -> {
                a.startActivity(Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:${a.packageName}")))
                result.success(null)
            }
            "installieren" -> {
                if (wartend != null) return result.error("laeuft", null, null)
                wartend = result
                val pfad = call.argument<String>("pfad")!!
                val sha = call.argument<String>("sha256")!!
                Thread {
                    try {
                        installieren(a, File(pfad), sha)
                    } catch (e: Exception) {
                        a.runOnUiThread { fertig(e.message ?: "Fehler") }
                    }
                }.start()
            }
            else -> result.notImplemented()
        }
    }

    private fun installieren(a: Activity, apk: File, sha: String) {
        if (!sha.equals(pruefsumme(apk), ignoreCase = true)) {
            apk.delete()
            throw IllegalStateException("pruefsumme")
        }
        val pi = a.packageManager.packageInstaller
        val params = PackageInstaller.SessionParams(PackageInstaller.SessionParams.MODE_FULL_INSTALL)
        params.setAppPackageName(a.packageName)
        params.setSize(apk.length())
        val id = pi.createSession(params)
        pi.openSession(id).use { s ->
            try {
                s.openWrite("schnellweg.apk", 0, apk.length()).use { aus ->
                    apk.inputStream().use { it.copyTo(aus) }
                    s.fsync(aus)
                }
                // FLAG_MUTABLE: das System trägt Status und Bestätigungs-Intent in den PendingIntent ein.
                val zurueck = PendingIntent.getBroadcast(a, id, Intent(a, Rueckmeldung::class.java),
                    PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_MUTABLE)
                s.commit(zurueck.intentSender)
            } catch (e: Exception) {
                s.abandon()
                throw e
            }
        }
    }

    private fun pruefsumme(f: File): String {
        val md = MessageDigest.getInstance("SHA-256")
        f.inputStream().use { ein ->
            val puffer = ByteArray(1 shl 16)
            while (true) {
                val n = ein.read(puffer)
                if (n < 0) break
                md.update(puffer, 0, n)
            }
        }
        return md.digest().joinToString("") { "%02x".format(it) }
    }

    private fun fertig(fehler: String?): Boolean {
        val r = wartend ?: return false
        wartend = null
        if (fehler == null) r.success(null) else r.error("install", fehler, null)
        return true
    }

    class Rueckmeldung : BroadcastReceiver() {
        override fun onReceive(c: Context, i: Intent) {
            when (val status = i.getIntExtra(PackageInstaller.EXTRA_STATUS, PackageInstaller.STATUS_FAILURE)) {
                PackageInstaller.STATUS_PENDING_USER_ACTION -> {
                    @Suppress("DEPRECATION")
                    val bestaetigen = i.getParcelableExtra<Intent>(Intent.EXTRA_INTENT)
                    if (bestaetigen == null) {
                        fertig("keine Bestätigung")
                        return
                    }
                    c.startActivity(bestaetigen.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
                    fertig(null) // Rest entscheidet der Nutzer im Dialog; Dart wartet nicht darauf
                }
                PackageInstaller.STATUS_SUCCESS -> fertig(null)
                else -> {
                    val text = i.getStringExtra(PackageInstaller.EXTRA_STATUS_MESSAGE) ?: "Status $status"
                    // Abbruch im Dialog ist kein Fehler. Nach dem Dialog wartet niemand mehr, dann als Toast.
                    if (!fertig(if (status == PackageInstaller.STATUS_FAILURE_ABORTED) null else text) &&
                        status != PackageInstaller.STATUS_FAILURE_ABORTED) {
                        Toast.makeText(c, "Update fehlgeschlagen: $text", Toast.LENGTH_LONG).show()
                    }
                }
            }
        }
    }
}
