// Identity.kt
package lab.ridgeradio

import android.app.Application
import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import okhttp3.OkHttpClient
import okhttp3.HttpUrl.Companion.toHttpUrl
import org.json.JSONObject
import org.webrtc.PeerConnectionFactory
import java.security.KeyStore
import java.util.concurrent.TimeUnit
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

class RadioApp : Application() {
    internal lateinit var radio: Radio
    internal var http = OkHttpClient.Builder().pingInterval(8, TimeUnit.SECONDS).build()
    override fun onCreate() {
        super.onCreate()
        PeerConnectionFactory.initialize(PeerConnectionFactory.InitializationOptions.builder(this).createInitializationOptions())
        radio = Radio(this)
    }
}
internal fun origin(raw: String): String {
    val url = raw.trim().toHttpUrl()
    require(url.isHttps && url.username.isEmpty() && url.password.isEmpty() && url.encodedPath == "/" &&
        url.query == null && url.fragment == null)
    return url.toString().trimEnd('/')
}
internal class Identity(context: Context) {
    val prefs = context.getSharedPreferences("radio", Context.MODE_PRIVATE)
    private fun key(): SecretKey {
        val store = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        (store.getKey("radio-session", null) as? SecretKey)?.let { return it }
        return KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore").apply {
            init(KeyGenParameterSpec.Builder("radio-session", KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM).setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE).build())
        }.generateKey()
    }
    fun save(host: String, user: JSONObject, token: String) {
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.ENCRYPT_MODE, key())
        val sealed = Base64.encodeToString(cipher.iv + cipher.doFinal(token.toByteArray()), Base64.NO_WRAP)
        val edit = prefs.edit()
        if (host != this.host || user.optString("id") != this.user.optString("id")) edit.clear()
        edit.putString("host", host).putString("user", user.toString()).putString("session", sealed).commit()
    }
    val host get() = prefs.getString("host", "").orEmpty()
    val user get() = JSONObject(prefs.getString("user", "{}")!!)
    fun token(): String? = runCatching {
        val raw = Base64.decode(prefs.getString("session", null) ?: return null, Base64.NO_WRAP)
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(128, raw.copyOfRange(0, 12)))
        String(cipher.doFinal(raw, 12, raw.size - 12))
    }.getOrNull()
    fun clear() { prefs.edit().clear().commit() }
}
