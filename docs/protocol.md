# Private radio protocol v1

An installation is one organization. A private address is display and lookup data. Commands carry immutable user IDs. All client control traffic uses certificate-validated HTTPS or WSS. A single process serializes endpoint reservations and relay authorization. Each new WebSocket gets a new server session and authoritative snapshot.

## Accounts and operator access

| Operation | Request | Success and failure |
|---|---|---|
| POST /v1/login | JSON fields `dial`, `password`, optional `invite` | Returns `user {id,dial,name}` and a 12-hour opaque bearer token. Invite redemption atomically sets the bcrypt password and consumes the invite. A successful login replaces the previous token and socket. Generic HTTP 401 for invalid credentials, 429 for throttling. |
| POST /v1/logout | Authorization Bearer header | Deletes the current token, closes the radio and returns `ok`. |
| GET /v1/radio | WebSocket upgrade with Authorization Bearer header | Requires an active device token. Immediately sends a snapshot. A new socket replaces any previous radio for that identity. |
| GET /healthz | No body | Returns readiness, protocol version and aggregate counters. HTTP 503 on a failed DB health check. |
| Local operator CLI | OS-authorized access to state directory | Issue a 24-hour invite, recover a handset, disable an account or create an online SQLite backup. |

A replacement retains the immutable account and approvals. This milestone uses a finite device token and an explicit sign-in on expiry. Refresh-token rotation is pending. Token digests are persisted server-side. Android encrypts the bearer with an AES-GCM key in Android Keystore.

## Signaling

Each UTF-8 JSON message has a `type`. UUID-like opaque strings are compared exactly. Signaling uses ordered, reliable WebSockets. Clients discard callbacks from obsolete socket generations. A socket's authenticated identity supplies the sender. Clients cannot specify a forwarding recipient on a media packet.

| Direction · type | Fields | Behavior |
|---|---|---|
| S → C · snapshot | session, people[], ice[], relayOnly | Each person includes id, dial, name, relation and presence. Relation is none, sent, received or approved. Presence is limited to approved contacts. ICE entries contain url and optional short-lived username/password. Refresh the directory on receipt. |
| C → S · contact | to, action | request, accept, remove. Only the other party can accept. Remove covers decline, withdrawal and revocation. It invalidates active private authorization under the floor lock. Both parties receive fresh directory state. |
| C → S · offer | role, id, sdp | A unique up/sendonly or down/recvonly WebRTC audio leg. Only one leg of each kind per socket. Server codec negotiation permits Opus at 48 kHz. |
| C → S · ice | role, id, candidate | Trickle ICE after that leg’s offer. Older leg IDs are ignored. |
| S → C · answer / path | role, id, sdp for answer | The answer includes gathered server candidates. Path indicates server-observed ICE/DTLS connectivity. Android also checks its local connection state. |
| C → S · listen | — | Sent after audio focus, downlink connection and snapshot. Establishes receive presence. |
| C → S · ping / S → C · pong | — | Clients heartbeat every five seconds. Server expires silent control sessions after 22 seconds. Socket close also removes presence. |
| C → S · press | request, seq, to, id | Fresh input creates a new request ID, strictly increasing sequence and current uplink ID. Named peer and media readiness are revalidated. First eligible request reserves both endpoints. |
| S → C · incoming | from, burst | Only the authorized receiver gets the speaker identity. It completes the incoming cue before sending ready. |
| C → S · ready / cant_play | burst | Only the matching listener and current pending burst can complete setup. |
| S → C · grant | request, burst, id | Matching held input and uplink permit grant chirp followed by capture. Other grants are released without capture. |
| S → C · denied | request, reason | Reasons include busy, unavailable, stale, slow_down and media. No press is queued. |
| C → S · renew / S → C · renewed | burst | Only the current talker can extend the live lease. The absolute 60-second deadline always wins. |
| C → S · release | request | Idempotent. Cancels setup or ends live transmission. Android stops capture locally first. |
| S → C · warning | burst | Sent five seconds before the absolute limit. |
| S → C · free | burst, request, reason, peer, reply, rotate | Clears live state. A speaker rebuilds its one-use uplink when rotate is true. Reply reserves both endpoints for six seconds. Reasons include released, ended, timeout, lease, limit, route, media, offline and unavailable. |
| C → S · end | — | Immediately leaves the current private conversation or reply window. |

Setup expires in three seconds. A renewable lease lasts 2.8 seconds, checked on every packet and on a 100 ms sweep. Client renewal interval is 650 ms. The client closes capture when it stops receiving renewal acknowledgments. Release retires the uplink’s cryptographic association before a subsequent press can send speech. New WebRTC connections are required after reconnect or restart.

The downlink stays warm. The relay admits a packet only from the current talker’s authorized leg and forwards it to the current receiver. It translates sequence numbers and timestamps to the receiver’s RTP stream, preserving gaps within a burst and discarding duplicate/late packets. WebRTC handles DTLS-SRTP, playout buffering and Opus concealment. Voice frames exist only in transit and in transient media buffers.

Groups, aliases, blocks, alerts, conversation history, preference sync and full field qualification belong to later milestones. Their wire contracts will be published with their implementations.
