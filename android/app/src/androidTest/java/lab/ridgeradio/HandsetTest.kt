// HandsetTest.kt
package lab.ridgeradio

import android.content.Intent
import android.os.SystemClock
import android.util.Base64
import android.view.MotionEvent
import android.view.View
import android.view.ViewGroup
import android.widget.Button
import androidx.test.platform.app.InstrumentationRegistry
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test
import java.io.File
import java.security.KeyStore
import java.security.cert.CertificateFactory
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicLong
import javax.net.ssl.*

class HandsetTest {
    private val inst = InstrumentationRegistry.getInstrumentation()
    private val app get() = inst.targetContext.applicationContext as RadioApp
    private fun main(f: () -> Unit) = inst.runOnMainSync(f)
    private fun until(label: String, test: () -> Boolean) {
        val stop = SystemClock.elapsedRealtime() + 20000
        while (SystemClock.elapsedRealtime() < stop) {
            var ok = false; main { ok = test() }; if (ok) return; SystemClock.sleep(40)
        }
        fail("Timed out: " + label + " (" + app.radio.state + ", " + app.radio.detail + ")")
    }
    @Test fun enrollLocalLab() {
        val file = File(app.cacheDir,"ridge-enroll.json")
        assumeTrue("Requires operator-provided local lab invite", file.exists())
        assertNull("A user is already signed in", app.radio.identity.token())
        val cfg = JSONObject(file.readText()); file.delete()
        val host = app.getString(R.string.service_hint)
        require(host.startsWith("https://") && host == cfg.getString("host"))
        val activity = inst.startActivitySync(Intent(app,MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)) as MainActivity
        fun all(v: View): List<View> = if (v is ViewGroup) (0 until v.childCount).flatMap { all(v.getChildAt(it)) } else listOf(v)
        main {
            val views = all(activity.window.decorView)
            val fields = views.filterIsInstance<android.widget.EditText>()
            assertEquals(4,fields.size)
            fields[0].setText(host); fields[1].setText(cfg.getString("dial"))
            fields[2].setText(java.util.UUID.randomUUID().toString()); fields[3].setText(cfg.getString("invite"))
            views.filterIsInstance<Button>().first { it.text == "Sign in or redeem invite" }.performClick()
        }
        until("normal HTTPS enrollment") { app.radio.identity.token() != null }
        assertFalse(app.radio.active)
        main { activity.finish() }
    }
    @Test fun privateTurnExchange() {
        val input = File(app.cacheDir, "ridge-bench.json")
        assumeTrue("Start the isolated TestUSBHandset fixture to run this test", input.exists())
        assertNull("Use an unused lab installation", app.radio.identity.token())
        val bench = JSONObject(input.readText()); input.delete()
        val cert = CertificateFactory.getInstance("X.509").generateCertificate(Base64.decode(bench.getString("cert"),0).inputStream())
        val store = KeyStore.getInstance(KeyStore.getDefaultType()).apply { load(null); setCertificateEntry("fixture",cert) }
        val tm = TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm()).apply { init(store) }.trustManagers.single() as X509TrustManager
        val ssl = SSLContext.getInstance("TLS").apply { init(null,arrayOf(tm),null) }
        val original = app.http
        app.http = OkHttpClient.Builder().sslSocketFactory(ssl.socketFactory,tm).build()
        val host = bench.getString("host")
        val login = obj("dial" to "01*12*1","password" to bench.getString("password"),"invite" to bench.getString("invite"))
        var activity: MainActivity? = null
        try {
            val req = Request.Builder().url(host + "/v1/login").post(login.toString().toRequestBody("application/json".toMediaType())).build()
            app.http.newCall(req).execute().use { r ->
                assertEquals(200,r.code); val answer = JSONObject(r.body.string())
                app.radio.identity.save(host,answer.getJSONObject("user"),answer.getString("token"))
            }
            activity = inst.startActivitySync(Intent(app,MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)) as MainActivity
            assertFalse(app.radio.active)
            until("visible activity") { activity.hasWindowFocus() }
            main { app.startForegroundService(Intent(app,RadioService::class.java).setAction("on")) }
            until("both WebRTC legs ready") { app.radio.state == "Ready" && app.radio.mic() }
            val am = app.getSystemService(android.media.AudioManager::class.java)
            fun recording() = am.activeRecordingConfigurations.any {
                it.clientAudioSource == android.media.MediaRecorder.AudioSource.VOICE_COMMUNICATION && !it.isClientSilenced
            }
            assertFalse("Microphone open in idle", recording())
            val peer=bench.getString("peer")
            main { app.radio.contact(peer,"request") }
            until("mutual approval") { app.radio.person(peer)?.optString("relation") == "approved" }
            main { app.radio.choose(peer) }
            fun find(v: View): Button? {
                if (v is Button && v.text == "Hold to talk") return v
                if (v is ViewGroup) for(i in 0 until v.childCount) find(v.getChildAt(i))?.let { return it }
                return null
            }
            lateinit var talk: Button
            main { talk = requireNotNull(find(activity.window.decorView)) }
            val down = SystemClock.uptimeMillis()
            fun touch(action: Int) = main {
                val e = MotionEvent.obtain(down,SystemClock.uptimeMillis(),action,20f,20f,0)
                talk.dispatchTouchEvent(e); e.recycle()
            }
            touch(MotionEvent.ACTION_DOWN)
            until("grant cue finished and microphone live") { app.radio.live }
            until("Android recording active") { recording() }
            SystemClock.sleep(900); touch(MotionEvent.ACTION_UP)
            until("Android recording stopped") { !recording() }
            assertFalse(app.radio.live)
            until("live reply identified") { app.radio.state == "Receiving" && app.radio.destination == peer }
            val stop = SystemClock.elapsedRealtime()+5000
            val count=AtomicLong()
            while(count.get()<3 && SystemClock.elapsedRealtime()<stop){
                val lat=CountDownLatch(1);main { app.radio.receivedPackets { count.set(it); lat.countDown() } }
                lat.await(1,TimeUnit.SECONDS);SystemClock.sleep(80)
            }
            assertTrue("Expected multiple inbound SRTP audio packets",count.get()>=3)
            android.util.Log.i("RidgeBench","Received " + count.get() + " Opus packets on handset")
        } finally {
            main { app.radio.stop(); app.stopService(Intent(app,RadioService::class.java)); activity?.finish(); app.radio.identity.clear() }
            app.http = original
        }
    }
}
