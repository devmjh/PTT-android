# Milestone acceptance record

Test date: 2026-11-24. Version 0.1.0 (1). The handset build targets API 37 and supports API 29+. The service used SQLite schema 1, Go 1.27.4 and Pion WebRTC 4.2.29. Tested builds and setup commands are in the README. A completed packet exchange establishes media transport. Human observations establish acoustic intelligibility and timing.

## Results by test environment

| Environment | Executed check | Result |
|---|---|---|
| Linux service integration | TestEncryptedPrivateExchange, real HTTPS, WSS, ICE, DTLS-SRTP, two independent Pion clients | Passed. Mutually approved peers exchanged known Opus payloads both ways. Sending before approval and after a canceled setup produced zero received frames. Late receiver readiness and repeated release did not restart capture. |
| Linux floor arbitration | TestPrivateContention100 | Passed 100 simultaneous **private** contention trials. Exactly 100 grants and 100 denials. A shared start barrier and concurrent calls exercised the real serialized handler. This test uses modeled media legs. |
| Linux timer tests | TestDeadlinesAndReply | Passed with an injected clock. Three-second request expiry, lease expiry, six-second reply reservation, 55-second warning, 60-second cutoff and explicit End. |
| SQLite identity and restore | TestDirectoryIdentityAndRecovery, TestBackupRestore | Passed. Private-ID validation, duplicate rejection, one-use redemption, replacement token revocation, stable identity and contact approval survive recovery. A restored database retains peer identity and approval. |
| Go race detector | go test -race -count=1 ./... | Passed all five host tests. The opt-in USB test was skipped in this invocation. |
| Android build | Gradle assembleDebug and assembleDebugAndroidTest, apksigner verify, zipalign -c -P 16 4 | Passed for the portable APK, host-specific lab APK and companion instrumentation APK. Native libraries cover arm64-v8a, armeabi-v7a, x86 and x86_64. |
| Clean source-only copy | Gradle wrapper assembleDebug and go build -trimpath, then go test -count=1 | Passed. All 36 Android tasks executed. The clean Go suite completed in 4.365 s. |
| Samsung SM-G990U1, Android 16, API 36 | TestUSBHandset plus Android HandsetTest.privateTurnExchange, production app code and embedded TURN/TCP over an ADB reverse | Passed. The final run took 4.93 s for the entire fixture. The software listener received **32 Opus packets** from the phone. The phone asserted at least three inbound Opus packets on the reply. Android reported no active voice-communication recording in idle, active recording during Talk and stopped recording after release. The fixture used a test-only TLS trust root. No acoustic recording or first-word measurement was made. |
| Lab LAN installation | Generated fresh CA, service and three invites. HTTPS health check. Normal login form via HandsetTest.enrollLocalLab on two physical phones | Passed on Samsung SM-G990U1 / API 36 and SM-A107M / Android 11 / API 30. Both enrolled via the LAN service’s scoped CA. Both reached the service over Wi-Fi on the same subnet. Both approved each other via the app and reached Ready with the other person selected and the speaker output displayed. Both report one loaded SIM slot. Both were USB-powered during the test. |
| Two physical Samsungs on the lab LAN | ADB pressed each native Talk button for 1.4 s, with B replying inside A’s six-second window. WSS and DTLS-SRTP used Wi-Fi. USB carried the input automation. | Passed transport in both directions. A→B raised the relay from 0 to 1 grants and 0 to 8 forwarded Opus packets. B→A raised it to 2 grants and 35 packets in total, adding 27 on reply. Both stayed connected with zero denials. These numbers establish relay forwarding and do not measure acoustic intelligibility. |
| Two-phone spoken exchange, lab Wi-Fi | A temporary handset test build supplied synthesized PCM to the normal WebRTC encoder after the grant chirp. Radio A sent “blue seven” to B, then B sent “green four” to A inside the reply window. | The listener confirmed clear audio in both directions and approved the radio cues. Radio B received 139 Opus packets and Radio A received 154. Both receivers reported nonzero decoded audio energy. Live-microphone first-word timing remains unmeasured. |

## Release gates

| Gate | Status | Evidence still required |
|---|---|---|
| A1 First conversation | Partial | Listener confirmed the synthesized phrase and immediate reply on the lab LAN. Repeat with a person speaking into each handset, check the displayed speaker, and exercise separate networks and Wi-Fi without a SIM. |
| A2 Contention and cancellation | Partial | Private concurrency, late readiness, repeat release and canceled media tested. Group contention and held-input handset trials remain. |
| A3 Groups and isolation | Pending | Persistent and selective groups follow A1. |
| A4 Conversation selection | Partial | Private reply and duplicate names exercised. Group precedence, aliases and One Touch pending. |
| A5 Failures and stale media | Partial | Timers, stale setup and forced TURN/TCP covered. Live network switching, server kill/recovery and separate NATs require field trials. |
| A6 Alerts and privacy | Pending | Alerts, DND and history follow A1. |
| A7 Android lifecycle | Partial | Physical foreground send/receive on API 36 and enrollment on API 30. Minimum API 29, target API 37, locked-screen, routes, interruptions, key and power matrix remain. |
| A8 Latency and reliability | Pending | Full 200-exchange series, controlled path and synchronized acoustic measurements required. |
| A9 Cold path and scale | Pending | Cold-start distribution, loss/delay profile and group load targets required. |
| A10 Operations and security | Partial | Build, identity, private media gating, inventory of stored tables and backup restore covered. Group restore/removal, full revocation and persistence/log review remain. |

## Requirement traceability

“Partial” records the named check and the remaining surface. Source review alone is explicitly labeled. The [roadmap](../ROADMAP.md) lists the planned milestones and their exit conditions.

| ID | Result | Evidence or remaining test |
|---|---|---|
| UX-01 | Partial | Handset enrollment, Radio off start and visible activation. Human route/consent review pending. |
| UX-02 | Partial | Native handset Talk button and directory selection used. Persistence and rotation matrix pending. |
| UX-03 | Partial | Grant/incoming callbacks exercised in HandsetTest. Four original cues, haptic and visual states in source. The listener approved the radio cues by ear on the physical phones. Per-cue haptic and visual checks remain. |
| UX-04 | Pending | A4 One Touch and restoration. |
| UX-05 | Partial | Touch down and release in HandsetTest, server-side cancellation in TestEncryptedPrivateExchange. Multi-pointer, navigation and device-loss matrix pending. |
| UX-06 | Pending | A7 dedicated-key/device qualification. |
| UX-07 | Partial | Accessible names and live-region labels in source. TalkBack and alternate bounded input pending. |
| ID-01 | Partial | TestDirectoryIdentityAndRecovery. Device replacement restores identity and approvals. Group and preference recovery pending. |
| ID-02 | Passed service tests | TestDirectoryIdentityAndRecovery and private exchange with duplicate display names. |
| ID-03 | Pending | A4 alias normalization, resolution and uniqueness. |
| ID-04 | Partial | Enrolled directory and explicit identity selection. Alias lookup, offline cache, contact naming, favorites and recents pending. |
| ID-05 | Partial | Authenticated listening presence, loss/close and busy revalidation in server path. Physical disappearance timing pending. |
| GR-01 | Pending | A3 persistent group administration and routing. |
| GR-02 | Pending | A3 tuning, mute and mid-burst changes. |
| GR-03 | Pending | A3 selective groups. |
| GR-04 | Partial | TestEncryptedPrivateExchange verifies approval before media. Blocks and group authorizations pending. |
| FL-01 | Partial | Private receiver readiness on real encrypted media paths. Group counts pending. |
| FL-02 | Partial | TestPrivateContention100. Cross-group conflicts and group media isolation pending. |
| FL-03 | Partial | HandsetTest verifies capture and inbound packets. Synthesized speech was intelligible through both handset speakers. Live microphone pickup immediately after the chirp and echo checks remain. |
| FL-04 | Partial | TestEncryptedPrivateExchange, TestDeadlinesAndReply, physical-to-software immediate reply. The listener heard the immediate two-phone reply with synthesized input on the lab LAN. |
| FL-05 | Partial | TestDeadlinesAndReply and TestEncryptedPrivateExchange. Field loss, server restart and delayed-grant handset trace pending. |
| FL-06 | Passed service timers | TestDeadlinesAndReply. On-handset warning/cutoff observation pending. |
| FL-07 | Partial | Private endpoint reservation in TestPrivateContention100. Group/private precedence pending. |
| FL-08 | Partial | Unauthorized, busy and timeout service paths. Field mic/route faults and last-listener drop matrix pending. |
| AL-01 | Pending | A6 alert actions. |
| AL-02 | Pending | A6 durable delivery, rate limit, deduplication and expiry. |
| AL-03 | Pending | A6 private per-user history and retention. |
| AL-04 | Partial | Radio off teardown in HandsetTest. DND and lock-screen qualification pending. |
| AN-01 | Partial | Normal foreground permission flow and OS recording checks. Denial/revocation and listen-only field trials pending. |
| AN-02 | Partial | Physical foreground service/send/receive. Screen-off, termination and cold-start qualification pending. |
| AN-03 | Partial | Speaker route used. Earpiece selection, wired/Bluetooth changes and interruption matrix pending. |
| AN-04 | Partial | Handset TURN/TCP and LAN enrollment exercised. Cellular/separate NAT, changeover and recovery pending. |
| AN-05 | Partial | Native WebRTC Opus and jitter buffer plus relay stale-leg gating exercised. Loss, quality and recovery measurements pending. |
| SV-01 | Partial | APK, service, embedded TURN, SQLite, operator CLI and three invites. Talkgroup creation pending. |
| SV-02 | Partial | Private v1 contract exercised by integration suite. Later-feature APIs and token refresh pending. |
| SV-03 | Partial | SQLite transactions and backup/restore, single process lock and ephemeral floors. Later-feature persistence and restart qualification pending. |
| SV-04 | Partial | Certificate-validated HTTPS/WSS, DTLS-SRTP and TURN/TCP on hardware. Broader isolation matrix pending. |
| SV-05 | Partial | bcrypt, Keystore AES-GCM, revocable device token, throttling, audit schema and recovery. Refresh tokens and complete object matrix pending. |
| SV-06 | Partial | Readiness and aggregate counters. Full privacy audit, retention configuration and metrics pending. |
| SV-07 | Partial | Fresh LAN init/build, physical enrollment, test commands and SQLite restore. Internet deployment and group restoration pending. |
