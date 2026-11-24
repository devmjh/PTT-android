// Media.kt
package lab.ridgeradio

import android.content.Context
import android.media.AudioAttributes
import android.media.MediaRecorder
import android.os.Handler
import org.json.JSONArray
import org.json.JSONObject
import org.webrtc.*
import org.webrtc.audio.JavaAudioDeviceModule
import java.util.UUID

internal fun obj(vararg fields: Pair<String, Any>): JSONObject =
    JSONObject().apply { fields.forEach { (key, value) -> put(key, value) } }

internal class Media(
    context: Context, private val main: Handler,
    private val send: (JSONObject) -> Unit, private val update: () -> Unit,
    private val fail: (String) -> Unit
) {
    private var closed = false
    private fun onMain(action: () -> Unit) { main.post { if (!closed) action() } }
    private fun micError() = onMain { fail("Microphone unavailable. Check permission and other audio apps.") }
    private fun routeError() = onMain { fail("Playback failed. Check the audio route and switch Radio on again.") }
    private val adm = JavaAudioDeviceModule.builder(context)
        .setAudioSource(MediaRecorder.AudioSource.VOICE_COMMUNICATION)
        .setAudioAttributes(AudioAttributes.Builder().setUsage(AudioAttributes.USAGE_VOICE_COMMUNICATION)
            .setContentType(AudioAttributes.CONTENT_TYPE_SPEECH).build())
        .setAudioRecordErrorCallback(object : JavaAudioDeviceModule.AudioRecordErrorCallback {
            override fun onWebRtcAudioRecordInitError(s: String) = micError()
            override fun onWebRtcAudioRecordError(s: String) = micError()
            override fun onWebRtcAudioRecordStartError(e: JavaAudioDeviceModule.AudioRecordStartErrorCode, s: String) = micError()
        }).setAudioTrackErrorCallback(object : JavaAudioDeviceModule.AudioTrackErrorCallback {
            override fun onWebRtcAudioTrackInitError(s: String) = routeError()
            override fun onWebRtcAudioTrackError(s: String) = routeError()
            override fun onWebRtcAudioTrackStartError(e: JavaAudioDeviceModule.AudioTrackStartErrorCode, s: String) = routeError()
        }).setEnableVolumeLogger(false).createAudioDeviceModule()
    private val factory = PeerConnectionFactory.builder().setAudioDeviceModule(adm).createPeerConnectionFactory()
    private class Leg(val id: String) {
        lateinit var pc: PeerConnection
        var sender: RtpSender? = null
        var track: AudioTrack? = null
        var source: AudioSource? = null
        var connected = false
        var serverReady = false
        var signaled = false
        val candidates = mutableListOf<IceCandidate>()
    }
    private val legs = mutableMapOf<String, Leg>()
    private var servers = emptyList<PeerConnection.IceServer>()
    private var relayOnly = false
    fun configure(ice: JSONArray, relay: Boolean) {
        servers = (0 until ice.length()).map {
            val spec = ice.getJSONObject(it)
            PeerConnection.IceServer.builder(spec.getString("url")).setUsername(spec.optString("username"))
                .setPassword(spec.optString("password")).createIceServer()
        }
        relayOnly = relay
    }
    fun path(role: String) = legs[role]?.let { it.connected && it.serverReady } == true
    fun id(role: String) = legs[role]?.id.orEmpty()

    fun open(role: String) {
        discard(role)
        val leg = Leg(UUID.randomUUID().toString())
        legs[role] = leg
        fun current(action: () -> Unit) = onMain { if (legs[role] === leg) action() }
        fun candidate(c: IceCandidate) = send(obj("type" to "ice", "role" to role, "id" to leg.id,
            "candidate" to obj("sdpMid" to c.sdpMid, "sdpMLineIndex" to c.sdpMLineIndex, "candidate" to c.sdp)))
        val cfg = PeerConnection.RTCConfiguration(servers).apply {
            sdpSemantics = PeerConnection.SdpSemantics.UNIFIED_PLAN
            if (relayOnly) iceTransportsType = PeerConnection.IceTransportsType.RELAY
        }
        leg.pc = factory.createPeerConnection(cfg, object : PeerConnection.Observer {
            override fun onConnectionChange(state: PeerConnection.PeerConnectionState) = current {
                when (state) {
                    PeerConnection.PeerConnectionState.CONNECTED -> { leg.connected = true; update() }
                    PeerConnection.PeerConnectionState.DISCONNECTED, PeerConnection.PeerConnectionState.FAILED ->
                        fail("Media path lost. Reconnecting.")
                    else -> Unit
                }
            }
            override fun onIceCandidate(c: IceCandidate) = current { if (leg.signaled) candidate(c) else leg.candidates.add(c) }
            override fun onSignalingChange(s: PeerConnection.SignalingState) {}
            override fun onIceConnectionChange(s: PeerConnection.IceConnectionState) {}
            override fun onIceConnectionReceivingChange(b: Boolean) {}
            override fun onIceGatheringChange(s: PeerConnection.IceGatheringState) {}
            override fun onIceCandidatesRemoved(c: Array<out IceCandidate>) {}
            override fun onAddStream(s: MediaStream) {}
            override fun onRemoveStream(s: MediaStream) {}
            override fun onDataChannel(c: DataChannel) {}
            override fun onRenegotiationNeeded() {}
        }) ?: error("WebRTC initialization failed")
        leg.pc.setAudioRecording(false)
        val dir = if (role == "up") RtpTransceiver.RtpTransceiverDirection.SEND_ONLY else RtpTransceiver.RtpTransceiverDirection.RECV_ONLY
        val trx = leg.pc.addTransceiver(MediaStreamTrack.MediaType.MEDIA_TYPE_AUDIO, RtpTransceiver.RtpTransceiverInit(dir))
        if (role == "up") leg.sender = trx.sender
        val sdpError = { current { fail("Media negotiation failed. Check ICE and TURN settings.") } }
        leg.pc.createOffer(observer(sdpError, created = { offer -> current {
            leg.pc.setLocalDescription(observer(sdpError, set = { current {
                send(obj("type" to "offer", "role" to role, "id" to leg.id, "sdp" to offer.description))
                leg.signaled = true
                leg.candidates.forEach(::candidate)
                leg.candidates.clear()
            } }), offer)
        } }), MediaConstraints())
    }
    private fun observer(failed: () -> Unit, created: (SessionDescription) -> Unit = {}, set: () -> Unit = {}) =
        object : SdpObserver {
            override fun onCreateSuccess(s: SessionDescription) = created(s)
            override fun onSetSuccess() = set()
            override fun onCreateFailure(s: String) = failed()
            override fun onSetFailure(s: String) = failed()
        }
    fun event(msg: JSONObject) {
        val leg = legs[msg.getString("role")] ?: return
        if (leg.id != msg.getString("id")) return
        if (msg.getString("type") == "path") { leg.serverReady = true; update(); return }
        leg.pc.setRemoteDescription(observer({ onMain { fail("Rejected media answer. Reconnecting.") } }),
            SessionDescription(SessionDescription.Type.ANSWER, msg.getString("sdp")))
    }
    fun capture(): Boolean {
        val leg = legs["up"] ?: return false
        if (!path("up") || leg.track != null) return false
        val source = factory.createAudioSource(MediaConstraints())
        leg.source = source
        val track = factory.createAudioTrack("talk", source)
        leg.track = track
        if (leg.sender?.setTrack(track, false) != true) return false
        leg.pc.setAudioRecording(true)
        return true
    }
    fun silence() {
        val leg = legs["up"] ?: return
        if (leg.track == null) return
        leg.track?.setEnabled(false)
        leg.pc.setAudioRecording(false)
        leg.sender?.setTrack(null, false)
        leg.track?.dispose(); leg.track = null
        leg.source?.dispose(); leg.source = null
    }
    private fun discard(role: String) {
        if (role == "up") silence()
        val leg = legs.remove(role) ?: return
        leg.pc.dispose()
    }
    fun receivedPackets(callback: (Long) -> Unit) {
        val pc = legs["down"]?.pc ?: return callback(0)
        pc.getStats { report ->
            val total = report.statsMap.values.filter { it.type == "inbound-rtp" }
                .sumOf { (it.members["packetsReceived"] as? Number)?.toLong() ?: 0L }
            callback(total)
        }
    }
    fun close() {
        closed = true
        discard("up"); discard("down")
        factory.dispose(); adm.release()
    }
}
