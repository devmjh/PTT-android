// MainActivity.kt
package lab.ridgeradio

import android.Manifest
import android.app.*
import android.content.Intent
import android.content.pm.PackageManager
import android.content.res.ColorStateList
import android.graphics.Color
import android.graphics.Typeface
import android.graphics.drawable.GradientDrawable
import android.media.AudioManager
import android.os.*
import android.text.InputType
import android.view.*
import android.widget.*
import okhttp3.*
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject
import java.io.IOException

class MainActivity : Activity() {
    private val app get() = application as RadioApp
    private val radio get() = app.radio
    private val audio get() = getSystemService(AudioManager::class.java)
    private lateinit var body: LinearLayout
    private lateinit var status: TextView
    private lateinit var detail: TextView
    private lateinit var destination: TextView
    private lateinit var dial: TextView
    private lateinit var route: TextView
    private lateinit var talk: Button
    private lateinit var power: Button
    private lateinit var end: Button
    private var home = false
    private var pointer = -1
    private val cream = Color.rgb(231,237,226)
    private val muted = Color.rgb(178,194,185)
    private val green = Color.rgb(169,229,111)
    private fun dp(value: Int) = (value * resources.displayMetrics.density).toInt()
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        volumeControlStream = AudioManager.STREAM_VOICE_CALL
        if (radio.identity.token() != null) buildHome() else loginScreen()
    }
    override fun onStart() { super.onStart(); radio.observer = { if (home && radio.identity.token() == null) loginScreen() else render() }; render() }
    override fun onPause() { radio.release(); pointer = -1; super.onPause() }
    override fun onStop() { radio.observer = null; super.onStop() }
    override fun onWindowFocusChanged(hasFocus: Boolean) { super.onWindowFocusChanged(hasFocus); if (!hasFocus && home) { radio.release(); pointer = -1 } }
    private fun layout() {
        val scroll = ScrollView(this).apply { isFillViewport = true }
        body = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL; setPadding(dp(22),dp(18),dp(22),dp(18)) }
        scroll.addView(body); setContentView(scroll)
        scroll.setOnApplyWindowInsetsListener { view, insets ->
            view.setPadding(insets.systemWindowInsetLeft, insets.systemWindowInsetTop, insets.systemWindowInsetRight, insets.systemWindowInsetBottom)
            insets
        }
    }
    private fun label(text: String, size: Int = 16, color: Int = cream) = TextView(this).apply {
        this.text = text; textSize = size.toFloat(); setTextColor(color); body.addView(this)
    }
    private fun gap(n: Int) { body.addView(Space(this), LinearLayout.LayoutParams(1,dp(n))) }
    private fun button(title: String, action: () -> Unit) = Button(this).apply {
        text = title; isAllCaps = false; setOnClickListener { action() }; body.addView(this)
    }
    private fun field(hintText: String, type: Int) = EditText(this).apply {
        hint = hintText; contentDescription = hintText; inputType = type; isSingleLine = true; body.addView(this)
    }
    private fun loginScreen() {
        home = false; layout(); gap(18)
        label("Ridge Radio",34).setTypeface(null,Typeface.BOLD); gap(8)
        label("Private push-to-talk",19,green); gap(18)
        label("Use your organization’s service address and private ID. For a new handset, sign in with your existing password.",16,muted)
        gap(16)
        val host = field("https://radio.example.org", InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_URI)
        host.setText(radio.identity.host.ifEmpty { getString(R.string.service_hint) })
        val id = field("Private ID · 164*123*4567", InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS)
        val pw = field("Password · at least 10 characters", InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_PASSWORD)
        val invite = field("Invite code · first sign-in only", InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_PASSWORD)
        val message = label(radio.detail,15,green)
        lateinit var sign: Button
        sign = button("Sign in or redeem invite") {
            val server = runCatching { origin(host.text.toString()) }.getOrNull()
            if (server == null) { message.text = "Enter an HTTPS server address."; return@button }
            val json = obj("dial" to id.text.toString(), "password" to pw.text.toString(), "invite" to invite.text.toString())
            val req = Request.Builder().url(server + "/v1/login").post(json.toString().toRequestBody("application/json".toMediaType())).build()
            sign.isEnabled = false; message.text = "Signing in…"
            app.http.newCall(req).enqueue(object : Callback {
                override fun onFailure(call: Call, e: IOException) { runOnUiThread { if (!isDestroyed) { sign.isEnabled = true; message.text = "Cannot reach the service. Check the address, TLS trust and network." } } }
                override fun onResponse(call: Call, response: Response) {
                    response.use {
                        val answer = if (it.isSuccessful) runCatching { JSONObject(it.body.string()) }.getOrNull() else null
                        runOnUiThread {
                            if (isDestroyed) return@runOnUiThread
                            sign.isEnabled = true
                            if (answer == null) { message.text = if (response.code == 429) "Too many attempts. Wait a moment." else "Sign-in failed. Check the ID, password and invite."; return@runOnUiThread }
                            radio.identity.save(server,answer.getJSONObject("user"),answer.getString("token")); buildHome()
                        }
                    }
                }
            })
        }
    }
    private fun buildHome() {
        home = true; layout()
        label("Ridge Radio",29).setTypeface(null,Typeface.BOLD)
        val me = radio.identity.user; label(me.optString("name") + "   " + me.optString("dial"),16,muted)
        gap(16)
        power = button("Radio on") { if (radio.active) off() else activate() }
        route = label("",15,muted); gap(14)
        status = label("",24,green); status.accessibilityLiveRegion = View.ACCESSIBILITY_LIVE_REGION_POLITE
        detail = label("",16,muted); gap(16)
        destination = label("",25); destination.setTypeface(null,Typeface.BOLD)
        dial = label("",16,muted); gap(6)
        talk = button("Hold to talk") {}
        talk.minHeight = dp(104); talk.textSize = 23f; talk.setTypeface(null,Typeface.BOLD)
        talk.setOnTouchListener { v, e ->
            when (e.actionMasked) {
                MotionEvent.ACTION_DOWN -> { pointer = e.getPointerId(0); if (radio.mic()) radio.press() else permission() }
                MotionEvent.ACTION_UP -> { pointer = -1; radio.release(); v.performClick() }
                MotionEvent.ACTION_POINTER_UP -> if (e.getPointerId(e.actionIndex) == pointer) { pointer = -1; radio.release() }
                MotionEvent.ACTION_CANCEL -> { pointer = -1; radio.release() }
                MotionEvent.ACTION_MOVE -> if (pointer >= 0) {
                    val i = e.findPointerIndex(pointer)
                    if (i < 0 || e.getX(i) !in 0f..v.width.toFloat() || e.getY(i) !in 0f..v.height.toFloat()) { pointer = -1; radio.release() }
                }
            }; true
        }
        gap(8); button("Choose contact") { picker() }
        end = button("End conversation") { radio.end() }
        button("Settings") { settings() }
        render()
    }
    private fun render() {
        if (!home || !::end.isInitialized) return
        status.text = radio.state; detail.text = radio.detail
        if (!radio.active && radio.detail.isEmpty()) detail.text = "Switch Radio on to hear live voice from approved contacts."
        power.text = if (radio.active) "Radio off" else "Radio on"
        route.text = if (radio.active) "Output · " + routeName(audio) else "Output · Radio off"
        val p = radio.person(radio.destination)
        destination.text = p?.optString("name") ?: "Choose a contact"
        dial.text = p?.optString("dial") ?: "Private call"
        talk.text = if (radio.live) "Talk now · release to finish" else "Hold to talk"
        talk.isEnabled = radio.active && p != null
        talk.contentDescription = talk.text.toString() + ", " + destination.text
        talk.background = GradientDrawable().apply { cornerRadius = dp(16).toFloat(); setColor(if (radio.live) 0xffffc163.toInt() else if (talk.isEnabled) green else 0xff465653.toInt()) }
        talk.setTextColor(if (talk.isEnabled) 0xff152019.toInt() else cream)
        end.visibility = if (radio.active && radio.destination.isNotEmpty()) View.VISIBLE else View.GONE
    }
    private fun activate() {
        AlertDialog.Builder(this).setTitle("Switch Radio on?")
            .setMessage("Approved contacts can play live voice through this phone without ringing. Your microphone opens only for a granted talk burst.")
            .setNegativeButton("Cancel", null).setPositiveButton("Radio on") { _,_ -> if (!radio.mic()) permission() else startRadio() }.show()
    }
    private fun permission() = requestPermissions(arrayOf(Manifest.permission.RECORD_AUDIO),31)
    override fun onRequestPermissionsResult(code: Int, names: Array<String>, results: IntArray) {
        super.onRequestPermissionsResult(code,names,results)
        if (code == 31) {
            if (radio.active && radio.mic()) { off(); radio.main.postDelayed({ if (!isFinishing && !isDestroyed && hasWindowFocus()) startRadio() },400) }
            else if (!radio.active) startRadio()
        }
    }
    private fun startRadio() { startForegroundService(Intent(this,RadioService::class.java).setAction("on")) }
    private fun off() { radio.stop(); stopService(Intent(this,RadioService::class.java)) }
    private fun picker() {
        radio.release()
        if (!radio.active || radio.people.length()==0) {
            Toast.makeText(this, if (radio.active) "No other enrolled contacts yet." else "Switch Radio on to see the directory.",Toast.LENGTH_LONG).show(); return
        }
        val entries = (0 until radio.people.length()).map { radio.people.getJSONObject(it) }
        val titles = entries.map {
            val rel = when(it.getString("relation")) { "approved" -> it.optString("presence"); "sent" -> "Request sent"; "received" -> "Approval requested"; else -> "Request contact" }
            it.getString("name") + " · " + it.getString("dial") + "\n" + rel
        }.toTypedArray()
        AlertDialog.Builder(this).setTitle("Private directory").setItems(titles) { _, i ->
            val p=entries[i]; val id=p.getString("id")
            val dialog=AlertDialog.Builder(this).setTitle(p.getString("name")).setMessage("Private ID  " + p.getString("dial"))
            when(p.getString("relation")) {
                "approved" -> dialog.setPositiveButton("Select") { _,_ -> radio.choose(id) }.setNeutralButton("Revoke") { _,_ -> radio.contact(id,"remove") }
                "received" -> dialog.setPositiveButton("Approve") { _,_ -> radio.contact(id,"accept") }.setNeutralButton("Decline") { _,_ -> radio.contact(id,"remove") }
                "sent" -> dialog.setNeutralButton("Withdraw request") { _,_ -> radio.contact(id,"remove") }
                else -> dialog.setPositiveButton("Request contact") { _,_ -> radio.contact(id,"request") }
            };dialog.setNegativeButton("Close",null).show()
        }.show()
    }
    private fun settings() {
        radio.release()
        val menu = mutableListOf("Sound cues","Vibration cues")
        if (Build.VERSION.SDK_INT >= 33) menu.add("Allow radio notifications")
        menu.add("Sign out")
        AlertDialog.Builder(this).setTitle("Radio settings").setItems(menu.toTypedArray()) { _,i ->
            when (menu[i]) {
                "Sign out" -> {
                    val token=radio.identity.token()
                    val req = Request.Builder().url(radio.identity.host + "/v1/logout").header("Authorization","Bearer " + token)
                        .post(ByteArray(0).toRequestBody()).build()
                    off(); radio.identity.clear(); loginScreen()
                    app.http.newCall(req).enqueue(object : Callback { override fun onFailure(c: Call,e: IOException) {} ; override fun onResponse(c: Call,r: Response) { r.close() } })
                }
                "Allow radio notifications" -> if (Build.VERSION.SDK_INT >= 33) requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS),32)
                else -> {
                    val key = if (i==0) "sounds" else "vibrate"
                    val enabled=radio.identity.prefs.getBoolean(key,true)
                    AlertDialog.Builder(this).setTitle(menu[i]).setSingleChoiceItems(arrayOf("On","Off"), if (enabled) 0 else 1) { d,n ->
                        radio.identity.prefs.edit().putBoolean(key,n==0).apply(); d.dismiss()
                    }.show()
                }
            }
        }.show()
    }
}
