// Radio.kt
package lab.ridgeradio

import android.Manifest
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.media.AudioAttributes
import android.media.AudioManager
import android.media.MediaPlayer
import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import android.os.VibrationEffect
import android.os.Vibrator
import okhttp3.*
import org.json.JSONArray
import org.json.JSONObject
import java.util.UUID
import kotlin.random.Random

internal class Radio(val app: RadioApp) {
    val identity = Identity(app)
    val main = Handler(Looper.getMainLooper())
    var observer: (() -> Unit)? = null
    var active = false; private set
    var detail = ""; private set
    var state = "Radio off"; private set
    var people = JSONArray(); private set
    var held = false; private set
    var live = false; private set
    private var snap = false
    private var listen = false
    private var ws: WebSocket? = null
    private var media: Media? = null
    private var attempt = 0
    private var epoch = 0L
    private var seq = 0L
    private var request = ""
    private var burst = ""
    private var incoming = ""
    private var bound = ""
    private var lastAck = 0L
    private var player: MediaPlayer? = null
    private var lastPong = 0L
    var selected: String
        get() = identity.prefs.getString("selected", "").orEmpty()
        private set(v) { identity.prefs.edit().putString("selected", v).apply() }
    val destination get() = bound.ifEmpty { selected }
    fun person(id: String): JSONObject? = (0 until people.length()).map { people.getJSONObject(it) }.find { it.optString("id") == id }
    fun mic() = app.checkSelfPermission(Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED
    fun receivedPackets(callback: (Long) -> Unit) { media?.receivedPackets(callback) ?: callback(0) }
    fun signal() { observer?.invoke() }
    private fun status(s: String, d: String = "") { state = s; detail = d; signal() }
    fun choose(id: String) {
        if (id == selected && bound.isEmpty()) return
        release()
        if (bound.isNotEmpty() || incoming.isNotEmpty()) send(obj("type" to "end"))
        bound = ""; incoming = ""; selected = id
        idle()
    }
    fun contact(id: String, action: String) = send(obj("type" to "contact", "to" to id, "action" to action))
    fun end() { release(); send(obj("type" to "end")); incoming = ""; bound = ""; stopCue(); idle() }
    fun start() { if (active) return; active = true; attempt = 0; connect() }
    private fun connect() {
        val token = identity.token() ?: return signedOut()
        val gen = ++epoch
        status(if (attempt == 0) "Connecting" else "Reconnecting")
        val req = Request.Builder().url(identity.host + "/v1/radio").header("Authorization", "Bearer " + token).build()
        ws = app.http.newWebSocket(req, object : WebSocketListener() {
            fun dispatch(f: () -> Unit) { main.post { if (epoch == gen && active) f() } }
            override fun onMessage(webSocket: WebSocket, text: String) = dispatch {
                runCatching { message(JSONObject(text)) }.onFailure { recover("Media or signaling setup failed.") }
            }
            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) = dispatch {
                if (response?.code == 401) signedOut() else recover("Check the server address, certificate and network.")
            }
            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) = dispatch { recover("Connection closed.") }
            override fun onClosing(webSocket: WebSocket, code: Int, reason: String) { webSocket.close(code, null) }
        })
        lastPong = SystemClock.elapsedRealtime()
        fun heartbeat() {
            if (!active || gen != epoch) return
            if (SystemClock.elapsedRealtime() - lastPong > 20000) { recover("Signaling timed out."); return }
            send(obj("type" to "ping")); main.postDelayed({ heartbeat() }, 5000)
        }
        main.postDelayed({ heartbeat() }, 5000)
        main.postDelayed({ if (gen == epoch && active && !listen) recover("ICE setup timed out. Check UDP or TURN.") }, 22000)
    }
    private fun destroyConnection() {
        epoch++; held = false; request = ""; burst = ""; incoming = ""; bound = ""
        snap = false; listen = false; live = false
        stopCue(); media?.close(); media = null
        ws?.cancel(); ws = null
    }
    fun recover(message: String) {
        if (!active) return
        destroyConnection()
        val gen = epoch
        status("Reconnecting", message)
        val delay = (350L shl attempt.coerceAtMost(5)) + Random.nextLong(250)
        attempt++
        main.postDelayed({ if (gen == epoch && active) connect() }, delay)
    }
    private fun signedOut() { stop("Session expired. Sign in again."); identity.clear(); signal(); app.stopService(Intent(app, RadioService::class.java)) }
    fun stop(message: String = "") {
        active = false; release(); destroyConnection(); people = JSONArray()
        status("Radio off", message)
    }
    private fun send(evt: JSONObject) { ws?.send(evt.toString()) }
    private fun pathsChanged() {
        val engine = media ?: return
        if (snap && engine.path("down") && !listen) {
            listen = true; attempt = 0; send(obj("type" to "listen"))
        }
        if (request.isEmpty() && incoming.isEmpty() && !held) idle()
    }
    private fun idle() {
        when {
            !active -> status("Radio off")
            !listen -> status("Connecting")
            !mic() -> status("Ready", "Listen only · allow microphone access to talk.")
            media?.path("up") != true -> status("Connecting", "Preparing transmit path")
            else -> status("Ready", if (bound.isEmpty()) "" else "Reply window open")
        }
    }
    fun press() {
        if (held) return
        held = true
        if (!active || !mic() || !listen || media?.path("up") != true || destination.isEmpty() || incoming.isNotEmpty()) {
            status(if (incoming.isNotEmpty()) "Receiving" else "Unavailable", "Check microphone, destination and radio status."); return
        }
        request = UUID.randomUUID().toString()
        val id = request
        status("Requesting")
        send(obj("type" to "press", "to" to destination, "id" to media!!.id("up"), "request" to id, "seq" to ++seq))
        main.postDelayed({
            if (request == id && burst.isEmpty()) {
                send(obj("type" to "release","request" to id)); request = ""
                denied("timeout")
            }
        }, 3000)
    }
    fun release() {
        if (!held && request.isEmpty()) return
        val id = request
        held = false; request = ""; burst = ""; live = false
        media?.silence()
        if (id.isNotEmpty()) { stopCue(); send(obj("type" to "release", "request" to id)); idle() }
        else if (state == "Requesting" || state == "Talk now") idle()
        signal()
    }
    private fun renew(id: String) {
        if (!held || burst != id) return
        if (!mic() || SystemClock.elapsedRealtime() - lastAck > 2350) {
            release(); status("Unavailable", "Microphone permission or floor lease lost. Press again."); return
        }
        send(obj("type" to "renew", "burst" to id)); main.postDelayed({ renew(id) }, 650)
    }
    private fun message(m: JSONObject) {
        when (m.getString("type")) {
            "pong" -> lastPong = SystemClock.elapsedRealtime()
            "snapshot" -> {
                people = m.getJSONArray("people")
                val old = person(selected)
                if (old == null || old.optString("relation") != "approved") selected = ""
                if (!snap) {
                    snap = true; media = Media(app, main, ::send, ::pathsChanged, ::recover)
                    media!!.configure(m.getJSONArray("ice"), m.optBoolean("relayOnly"))
                    media!!.open("down"); if (mic()) media!!.open("up")
                } else media?.configure(m.getJSONArray("ice"), m.optBoolean("relayOnly"))
                signal()
            }
            "answer", "path" -> media?.event(m)
            "incoming" -> {
                val id = m.getString("burst")
                val peer = person(m.getString("from"))
                if (!listen || peer?.optString("relation") != "approved") {
                    send(obj("type" to "cant_play", "burst" to id)); return
                }
                // A crossed outgoing press can still receive its Busy response. Keep
                // the incoming cue and readiness callback intact.
                if (request.isNotEmpty()) {
                    send(obj("type" to "release", "request" to request))
                    request = ""
                }
                bound = peer.getString("id"); incoming = id
                status("Receiving", peer.getString("name"))
                cue(R.raw.incoming) {
                    if (incoming == id && listen) send(obj("type" to "ready", "burst" to id))
                }
            }
            "grant" -> {
                val req = m.getString("request"); val id = m.getString("burst")
                if (!held || request != req || !mic() || m.getString("id") != media?.id("up")) {
                    send(obj("type" to "release", "request" to req)); return
                }
                burst = id; lastAck = SystemClock.elapsedRealtime(); renew(id)
                cue(R.raw.grant) {
                    if (held && request == req && burst == id && mic()) {
                        if (media?.capture() == true) { live = true; status("Talk now", "Microphone live") }
                        else { release(); status("Unavailable", "Microphone failed. Press again.") }
                    }
                }
            }
            "renewed" -> if (burst == m.getString("burst")) lastAck = SystemClock.elapsedRealtime()
            "warning" -> if (burst == m.getString("burst")) status("Talk now", "5 seconds remaining")
            "denied" -> if (m.getString("request") == request) { request = ""; denied(m.getString("reason")) }
            "free" -> {
                val id = m.getString("burst")
                if (m.getString("request") == request || burst == id) {
                    request = ""; burst = ""; live = false; media?.silence(); stopCue()
                }
                if (incoming == id) { incoming = ""; stopCue() }
                bound = if (m.getBoolean("reply")) m.getString("peer") else ""
                if (m.optBoolean("rotate") && mic()) media?.open("up")
                val reason = m.getString("reason")
                if (reason in setOf("released", "ended")) { idle(); if (reason == "released") cue(R.raw.free) {} }
                else denied(reason)
            }
        }
    }
    private fun denied(reason: String) {
        val text = when (reason) {
            "busy" -> "The other radio is in a conversation."
            "timeout" -> "The listener did not become ready within 3 seconds."
            "lease", "media" -> "Media path lost. Wait for Ready and press again."
            "limit" -> "60-second time limit reached. Release Talk."
            "route" -> "The listener cannot play audio."
            "slow_down" -> "Release and wait a moment before trying again."
            else -> "The contact is unavailable. Check approval and Radio on."
        }
        status(if (reason == "busy") "Busy" else "Unavailable", text)
        cue(R.raw.denied) {}
    }
    private fun stopCue() { player?.release(); player = null }
    private fun cue(resource: Int, done: () -> Unit) {
        stopCue()
        if (identity.prefs.getBoolean("vibrate", true)) {
            val v = app.getSystemService(Context.VIBRATOR_SERVICE) as Vibrator
            v.vibrate(VibrationEffect.createOneShot(if (resource == R.raw.denied) 80 else 25, VibrationEffect.DEFAULT_AMPLITUDE))
        }
        if (!identity.prefs.getBoolean("sounds", true)) { done(); return }
        val attrs = AudioAttributes.Builder().setUsage(AudioAttributes.USAGE_VOICE_COMMUNICATION)
            .setContentType(AudioAttributes.CONTENT_TYPE_SONIFICATION).build()
        val tone = MediaPlayer.create(app, resource, attrs, AudioManager.AUDIO_SESSION_ID_GENERATE)
        if (tone == null) { recover("Radio cue playback failed. Check audio output."); return }
        player = tone
        tone.setOnCompletionListener { if (player === tone) { stopCue(); done() } }
        tone.setOnErrorListener { _,_,_ -> if (player === tone) recover("Audio output failed."); true }
        tone.start()
    }
}
