// RadioService.kt
package lab.ridgeradio

import android.app.*
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.media.*
import android.net.*
import android.os.Build
import android.os.IBinder

internal fun routeName(a: AudioManager): String {
    val t = if (Build.VERSION.SDK_INT >= 31) a.communicationDevice?.type else null
    return when (t) {
        AudioDeviceInfo.TYPE_BUILTIN_EARPIECE -> "Earpiece"
        AudioDeviceInfo.TYPE_BUILTIN_SPEAKER -> "Speaker"
        AudioDeviceInfo.TYPE_BLUETOOTH_SCO, AudioDeviceInfo.TYPE_BLE_HEADSET -> "Bluetooth headset"
        AudioDeviceInfo.TYPE_WIRED_HEADSET, AudioDeviceInfo.TYPE_WIRED_HEADPHONES, AudioDeviceInfo.TYPE_USB_HEADSET -> "Wired headset"
        else -> if (a.isWiredHeadsetOn) "Wired headset" else if (a.isSpeakerphoneOn) "Speaker" else "System output"
    }
}
class RadioService : Service() {
    private val radio get() = (application as RadioApp).radio
    private val audio get() = getSystemService(AudioManager::class.java)
    private val network get() = getSystemService(ConnectivityManager::class.java)
    private var started = false
    private var reason = ""
    private var previousMode = AudioManager.MODE_NORMAL
    private var previousSpeaker = false
    private var currentNetwork: Network? = null
    private var currentRoute: AudioDeviceInfo? = null
    private val devices = object : AudioDeviceCallback() {
        override fun onAudioDevicesRemoved(removed: Array<AudioDeviceInfo>) {
            if (removed.any { it.isSink && (it.id == currentRoute?.id || (Build.VERSION.SDK_INT < 31 && privateRoute(it))) })
                shutdown("Private audio route disconnected. Confirm the new output by switching Radio on.")
        }
        override fun onAudioDevicesAdded(added: Array<AudioDeviceInfo>) { radio.signal() }
    }
    private fun privateRoute(d: AudioDeviceInfo) = d.type in setOf(AudioDeviceInfo.TYPE_WIRED_HEADSET, AudioDeviceInfo.TYPE_WIRED_HEADPHONES,
        AudioDeviceInfo.TYPE_USB_HEADSET, AudioDeviceInfo.TYPE_BLUETOOTH_SCO, AudioDeviceInfo.TYPE_BLE_HEADSET)
    private val networks = object : ConnectivityManager.NetworkCallback() {
        override fun onAvailable(n: Network) { radio.main.post { if (started && currentNetwork != null && currentNetwork != n) radio.recover("Network changed."); currentNetwork = n } }
        override fun onLost(n: Network) { radio.main.post { if (started && currentNetwork == n) { currentNetwork = null; radio.recover("Network disconnected.") } } }
    }
    private val focus by lazy { AudioFocusRequest.Builder(AudioManager.AUDIOFOCUS_GAIN)
        .setAudioAttributes(AudioAttributes.Builder().setUsage(AudioAttributes.USAGE_VOICE_COMMUNICATION)
            .setContentType(AudioAttributes.CONTENT_TYPE_SPEECH).build())
        .setOnAudioFocusChangeListener { if (it < 0) shutdown("Audio focus interrupted. Switch Radio on to resume.") }.build() }
    private fun shutdown(message: String) { reason = message; radio.stop(message); stopSelf() }
    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action != "on") { shutdown(""); return START_NOT_STICKY }
        val nm = getSystemService(NotificationManager::class.java)
        nm.createNotificationChannel(NotificationChannel("live", "Radio session", NotificationManager.IMPORTANCE_LOW))
        val off = PendingIntent.getService(this, 1, Intent(this, RadioService::class.java).setAction("off"), PendingIntent.FLAG_IMMUTABLE)
        val open = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
        val note = Notification.Builder(this, "live").setSmallIcon(R.drawable.radio_icon)
            .setContentTitle("Ridge Radio is on").setContentText("Listening for approved contacts")
            .setVisibility(Notification.VISIBILITY_PRIVATE).setOngoing(true).setContentIntent(open)
            .addAction(Notification.Action.Builder(null, "Radio off", off).build()).build()
        try {
            var types = ServiceInfo.FOREGROUND_SERVICE_TYPE_MEDIA_PLAYBACK
            if (Build.VERSION.SDK_INT >= 30 && radio.mic()) types = types or ServiceInfo.FOREGROUND_SERVICE_TYPE_MICROPHONE
            startForeground(7, note, types)
        } catch (e: RuntimeException) { shutdown("Open the app and allow audio permissions to resume."); return START_NOT_STICKY }
        if (started) return START_NOT_STICKY
        if (audio.requestAudioFocus(focus) != AudioManager.AUDIOFOCUS_REQUEST_GRANTED) { shutdown("Audio focus unavailable. End the other audio session."); return START_NOT_STICKY }
        started = true; previousMode = audio.mode; previousSpeaker = audio.isSpeakerphoneOn
        audio.mode = AudioManager.MODE_IN_COMMUNICATION
        if (Build.VERSION.SDK_INT >= 31) {
            val options = audio.availableCommunicationDevices
            val choice = options.firstOrNull { privateRoute(it) } ?: options.firstOrNull { it.type == AudioDeviceInfo.TYPE_BUILTIN_SPEAKER }
            if (choice != null) audio.setCommunicationDevice(choice)
            currentRoute = choice ?: audio.communicationDevice
        } else audio.isSpeakerphoneOn = !audio.isWiredHeadsetOn
        audio.registerAudioDeviceCallback(devices, radio.main)
        network.registerDefaultNetworkCallback(networks)
        radio.start()
        return START_NOT_STICKY
    }
    override fun onDestroy() {
        radio.stop(reason)
        if (started) {
            started = false
            network.unregisterNetworkCallback(networks); audio.unregisterAudioDeviceCallback(devices)
            if (Build.VERSION.SDK_INT >= 31) audio.clearCommunicationDevice() else audio.isSpeakerphoneOn = previousSpeaker
            audio.mode = previousMode; audio.abandonAudioFocusRequest(focus)
        }
        stopForeground(STOP_FOREGROUND_REMOVE); super.onDestroy()
    }
    override fun onBind(intent: Intent?): IBinder? = null
}
