# Ridge Radio

Ridge Radio is a private push-to-talk service and native Android app. Select an approved person, hold Talk, wait for the two-note permission chirp, speak, and release. The receiver hears live audio and has six seconds to reply to the caller with one press. The service uses a trusted WebRTC relay, Opus, TLS signaling and TURN. The four original handset-style cues can be previewed in `android/app/src/main/res/raw/`.

**Version 0.1.0** is the first private-call prerelease. Two physical Samsung phones exchanged clear synthesized phrases over the same Wi-Fi. A separate test verified physical microphone capture, release and a reply from a software peer over TURN/TCP. Human-microphone conversation across separate networks and the Wi-Fi-only/no-SIM repeat are still open under gate A1. [Acceptance record](docs/acceptance.md) · [Feature roadmap](ROADMAP.md)

## Implementation

| Component | Language and technology | Responsibility |
|---|---|---|
| [Android client](android/app/src/main/java/lab/ridgeradio) | Kotlin, native Android Views, Android foreground service, OkHttp 5.5.1 and WebRTC Android 150.7871.08 | Enrollment, contact approval, radio and Talk controls, communication audio routing, cues and microphone lifecycle. Android Keystore protects the key used to encrypt the device token. |
| [Service and operator CLI](service) | Go 1.27.4, Gorilla WebSocket, Pion WebRTC v4 and Pion TURN v5 | Account and directory API, authenticated signaling, exclusive permission to speak, Opus packet forwarding and short-lived TURN credentials. One Linux process owns the floor for an organization. |
| [Account storage](service/store.go) | SQLite with SQL transactions through Go’s CGO SQLite driver | Stores users, bcrypt password hashes, invite and token digests, contact approvals and operator audit records. Device presence and permission to speak live in memory. |
| [Deployment helpers](deploy) and [sound generator](tools/make-cues.py) | Python 3, POSIX shell and OpenSSL | Initialize a test organization, generate local TLS material, enroll test handsets and reproduce the four bundled WAV cues. |

**What happens when you press Talk.** The app sends a request over an authenticated TLS WebSocket. The service checks the relationship, reserves both radios and waits for the listener to finish its incoming cue. The talker then hears the grant chirp and starts transmitting Opus audio over DTLS-SRTP. The trusted relay forwards packets only to the authorized listener. Releasing Talk stops capture and opens the six-second reply window. ICE selects the media path, with TURN available for restrictive networks. [API, media and floor protocol](docs/protocol.md)

## Install and make a private call

Download the portable Android debug APK from the versioned release. Enter the HTTPS address of your own Ridge Radio service and use an invite from its operator. For a local test installation, the sections below cover the service and an optional APK that trusts just your generated CA at the chosen address. [Download v0.1.0](https://github.com/devmjh/PTT-android/releases/tag/v0.1.0)

1. Open Ridge Radio. Redeem the invite for your private ID with a new password of 10–72 UTF-8 bytes. A returning user can leave the invite field empty. A replacement sign-in disconnects the previous handset.
2. Tap **Radio on**, confirm, and allow the microphone while using the app. Denying the microphone allows listening. A foreground notification provides **Radio off** when notifications are enabled.
3. With both radios on, A opens **Choose contact**, checks B’s name and private ID, then selects **Request contact**. B opens the directory, selects A and chooses **Approve**.
4. A selects B and holds **Talk**. Speak after the permission chirp. Releasing Talk closes capture. B can reply immediately. **End conversation** clears the reply target. The directory also allows withdrawing, declining and revoking approval.
5. For the acoustic check, put the phones in separate rooms or use a qualified route to avoid hearing the speaker directly. Say “blue seven.” Have B report both words, the displayed speaker and whether either word repeated or clipped. B replies with a different phrase in the six-second window. Repeat on separate routed networks and with a Wi-Fi-only handset without a SIM.

## Get the prerelease binaries

From a clone of this repository on x86-64 Linux with glibc 2.34 or newer, the GitHub CLI can fetch both binaries and their checksums. The operator CLI and the server share the same executable.

```sh
gh release download v0.1.0 --repo devmjh/PTT-android --dir dist
(cd dist && sha256sum -c SHA256SUMS)
install -m 755 dist/ridge-linux-amd64 dist/ridge
adb install -r dist/Ridge-Radio-debug.apk
```

Use the LAN initialization steps below to issue invites and configure HTTPS. For other Linux architectures, compile the service on the target system.

## Build from source

Use Linux with Go 1.27.4, a C compiler and libc development headers for SQLite, JDK 21, Android SDK Platform 37.2, Build Tools 37.0.0 and Android Platform Tools. Set ANDROID_HOME to the SDK root. Gradle 9.6.0 and Android Gradle Plugin 9.5.2 compile Kotlin with the plugin’s built-in Kotlin support. The Android minimum API is 29 and target API is 37. WebRTC Android is 150.7871.08, OkHttp is 5.5.1 and Pion WebRTC is v4.2.29.

Run these commands from the repository root. The LAN setup scripts also require Python 3 and OpenSSL. USB installation requires ADB.

```sh
mkdir -p dist
(cd service && go build -trimpath -buildvcs=false -o ../dist/ridge .)
(cd android && ./gradlew :app:assembleDebug :app:assembleDebugAndroidTest)
cp android/app/build/outputs/apk/debug/app-debug.apk dist/Ridge-Radio-debug.apk
```

The target follows Android 17. The native audio engine and its Android packaging come from the WebRTC SDK project. [Android 17 SDK](https://developer.android.com/about/versions/17/setup-sdk) · [WebRTC Android SDK](https://github.com/webrtc-sdk/android)

The Gradle wrapper verifies its distribution checksum. Go verifies module checksums in `service/go.sum`. Gradle creates a machine-local debug signing key. Updating an existing installation requires the same signing identity.

## Initialize a private LAN service

Run the initializer once with the server’s actual static or reserved IPv4 address. The example address is reserved for documentation. The initializer creates a private state directory, SQLite schema, CA, TLS certificate, TURN secret and three distinct 24-hour invites. It refuses to overwrite an existing instance.

```sh
./deploy/init-lab.py 192.0.2.10
./deploy/serve-lab
```

The launcher uses the binary at `dist/ridge`. It serves HTTPS/WSS on TCP 8443, WebRTC on UDP 40000, and embedded TURN on UDP and TCP 3478. TURN allocates UDP ports 50000–50100. The generated configuration binds the service and its listeners to the selected interface. From a second shell, query the authenticated TLS endpoint using the local public CA.

```sh
. .local/instance/server.env
curl --cacert .local/instance/ca.pem "https://$RIDGE_BIND/healthz"
```

For a repeatable lab APK with host-specific CA trust and the URL prefilled, run the following after initialization.

```sh
(cd android && ./gradlew "-PridgeLabRes=$(realpath ../.local/instance/android-res)" :app:assembleDebug :app:assembleDebugAndroidTest)
cp android/app/build/outputs/apk/debug/app-debug.apk dist/Ridge-Radio-lab-debug.apk
cp android/app/build/outputs/apk/androidTest/debug/app-debug-androidTest.apk dist/Ridge-Radio-instrumentation.apk
```

Invite files in the private instance directory contain the name, private ID and one-time invite on separate lines. Give each person their own invite. The portable debug APK can instead use a CA installed by the handset owner, or a publicly trusted server certificate. Only the public CA certificate belongs on a handset.

For disposable lab accounts, `./deploy/enroll-usb.py DEVICE_SERIAL 1` installs the lab build, enters the first invite through the normal sign-in screen and leaves Radio off. Use slot 2 for B. This debug-only helper generates a random password and discards it. Use operator recovery to choose a personal password. Manual enrollment supports a user-chosen password from the start.

## Internet deployment

Use a Linux server reachable by both phones. Set `RIDGE_BIND`, `RIDGE_MEDIA_BIND`, `RIDGE_PUBLIC_IP`, `RIDGE_CERT`, `RIDGE_KEY` and `RIDGE_DB` as in the [sample environment](deploy/server.env.example). Place a CA-valid certificate chain and private key on the server. Point the app at the DNS name on the certificate. On a NAT gateway forward TCP 8443, UDP 40000, UDP/TCP 3478 and UDP 50000–50100 to that host, preserving port numbers. Devices on other networks must reach the advertised IP. A private LAN address requires an existing routed path or VPN. Hairpin NAT or split DNS may be needed for LAN clients of a public hostname.

The embedded TURN service issues one-hour REST credentials and permits allocations to send to the media relay IP. The media remains DTLS-SRTP over either client transport. Set `RIDGE_RELAY_ONLY=1` to qualify a relay-only path. For restrictive networks an external coturn with a trusted TLS certificate can expose `turns:turn.example.org:5349?transport=tcp` or a dedicated port 443 endpoint. Configure coturn with `use-auth-secret`, a private `static-auth-secret`, a realm and the correct public relay address. Supply the identical secret via `RIDGE_TURN_SECRET`, set the comma-separated `RIDGE_ICE_URLS` and omit `RIDGE_TURN_BIND` to use external TURN. Separate public-network verification is tracked in the acceptance record.

A TLS-terminating reverse proxy may forward the HTTPS API and WebSocket upgrade to loopback. Without certificate paths, `ridge serve` accepts a loopback bind only. The SQLite file and its lock must be on local storage with one service process per DB. Run the process under a dedicated OS account and restart it with your service manager. Restrict state and backup directories to that account.

## Operator and recovery

The CLI authenticates through local OS access to the private database directory. Run it as the instance owner. Source the instance environment first.

```sh
. .local/instance/server.env
./dist/ridge invite '164*123*4567' 'Dispatch test'
./dist/ridge recover '164*123*4567'
./dist/ridge disable '164*123*4567'
./dist/ridge backup /secure/backup/ridge-20261124.db
```

Invite and recover print a single-use code to the operator. Recovery preserves the private ID and approvals, revokes the previous token and issues a fresh enrollment. Disable invalidates credentials. Bring up a restored backup at a **new** RIDGE_DB path with the original process stopped. Delete the rows in the restored `tokens` table before opening the restored service to clients. Restore the TLS keys and TURN secret separately from protected operator storage. The schema migrates on startup and records `PRAGMA user_version`. Leases, media and presence are memory-only.

## Verification and troubleshooting

```sh
(cd service && go test -buildvcs=false -race -v ./...)
adb install -r dist/Ridge-Radio-lab-debug.apk
adb install -r -t dist/Ridge-Radio-instrumentation.apk
(cd service && RIDGE_HANDSET=1 ANDROID_SERIAL=YOUR_SERIAL go test -buildvcs=false -count=1 -v -run '^TestUSBHandset$')
```

The USB fixture requires an unused signed-out debug installation and an unlocked handset. It briefly opens the microphone during an intentional test press, counts packets, and sends a synthesized tone back. It creates temporary TLS and TURN endpoints and removes the ADB reverses afterward. Grant microphone permission only on the lab device being tested. The fixture signs out its own transient identity.

| Observation | What to check |
|---|---|
| Sign-in cannot reach service | Verify the HTTPS URL and route, certificate SAN, trusted CA, firewall and health response. A cellular handset needs a route to the service. |
| Radio stays Connecting or Reconnecting | Verify UDP 40000 and advertised address. Test TURN/TCP, shared secret, clock, allocation ports and relay-only mode. |
| Contact unavailable | Both users must finish enrollment, switch Radio on and approve the relationship. Open the directory again to refresh the displayed choices. |
| Busy | Release and make a fresh press after the other conversation’s reply window, or have that user end it. |
| Listen only | Grant microphone permission while the app is visible, then switch Radio off and on. Check for another app using the microphone. |
| Phone missing from ADB | Unlock it, enable Developer options and USB debugging, accept the computer prompt and use a USB data cable. |

The health endpoint reports aggregate counts. This milestone has no recording, voicemail or replay path. The test evidence identifies exactly which physical and deployment cases were exercised.
