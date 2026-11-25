# Ridge Radio roadmap

Version 0.1.0 supports one-to-one, invite-only push-to-talk. Two Samsung phones on the same Wi-Fi exchanged intelligible synthesized speech through the service. A separate handset test exercised microphone capture and TURN/TCP with a software peer. The next milestone is a human conversation across networks. [Recorded results by requirement and gate](docs/acceptance.md)

## 1. Move registration to a dedicated VM and qualify private calls · A1

### Registration server VM

Deploy the existing Ridge Radio service on one Linux VM on a lab hypervisor. The VM will hold the organization’s accounts and contact approvals and run signaling, floor control and the trusted media relay. Phones will use a stable HTTPS hostname so moving the VM later does not require changing the address on every handset.

| Step | Planned work | How to verify it |
|---|---|---|
| Choose placement and routing | Select a hypervisor after checking capacity and backup coverage. Reserve a guest address and DNS name. Start on the LAN and plan a VPN pilot for remote phones. Decide separately whether direct Internet access is needed. | Both phones resolve the name, validate its TLS certificate and reach signaling and WebRTC media from their intended networks. Exercise TURN as a separate test. |
| Harden the host and service | Use a supported, patched guest OS, key-based SSH from the management network, and an unprivileged systemd service. Allow only the required signaling, media and TURN traffic from the intended networks. Protect the database, TLS key and TURN secret. Assign owners for updates, certificate renewal and monitoring. | Reboot the VM and verify automatic startup. Check firewall reachability from handset and management networks, login throttling, short-lived TURN credentials and logs for accidental credentials or voice data. |
| Enroll and reconnect phones | Use the existing app flow. The operator gives each new user the service URL, a private PTT ID and an expiring, single-use invite. The app validates the server certificate and hostname. The user redeems the invite and sets a password. On later connections, **Radio on** establishes listening presence using the signed-in session. | Enroll a clean handset, reject a reused or expired invite, and confirm that replacement sign-in revokes the old handset and starts the new one with Radio off. |
| Migrate and recover | Back up the current SQLite database, rehearse an isolated restore and invalidate restored session tokens. Move existing accounts and approvals in a scheduled cutover with one active floor authority. Configure the phones for the new hostname and certificate, then sign in again. Document backup retention, rollback and recovery of service secrets. | Verify that the original PTT IDs and contact approvals survive migration, stale sessions are rejected, and both users can call after a VM restart. Record results alongside A1 and the A10 recovery evidence. |

### Two-handset field check

- Put two physical handsets on separate routed networks. Enroll both, approve the contact, switch both radios on and select the other person.
- Say “blue seven” into the first handset immediately after the chirp. Check that the listener hears both words once, sees the caller’s identity and can reply within six seconds. Repeat with a Wi-Fi-only handset without a SIM.
- Record the devices, Android versions, network paths, route and listener’s observations. Complete A1 before introducing the group interface.

## 2. Build group radio and contact features · A2–A6

| Feature | What people will be able to do | Acceptance check |
|---|---|---|
| Persistent talkgroups | Create a group, manage its roster, tune to one group, mute it or leave. An eligible listener can join during a burst. | A single speaker wins each of 100 group contention trials. Removed or untuned members stop receiving audio. |
| Selective group calls | Pick two or more approved contacts for a temporary conversation, see who is reachable and reply to the group. | Check partial availability, blocks between invitees and isolation from nonparticipants. |
| Contact selection and One Touch | Resolve private IDs and aliases, search the directory, favorite or rename contacts and select a last or assigned destination. | An incoming private call retains its reply target while an unrelated group is active. The previous selection returns when the window expires. |
| Call Alerts and history | Ask an approved contact to call back, see delivery status, save an alert and return a recent call. | Check deduplication, the 24-hour pending-alert expiry, DND, blocking and the 30-day history limit. |
| Recovery during a call | Resume listening after transport changes and service restarts. A new transmission requires a fresh press. | Force TURN, lose the speaker mid-burst and switch networks. Check lease expiry and that old audio cannot play. |

## 3. Qualify Android field use · A7

Test Android 10 (API 29), the target API and physical handset/accessory combinations. Qualify 15 minutes of screen-off listening, dedicated PTT key down/up events, Bluetooth and wired route loss, system-call interruptions, permission changes and forced stops. Exercise TalkBack, text scaling and the opt-in bounded start/stop action. Publish the results by device, accessory and foreground or locked state.

## 4. Measure performance and finish the operator handoff · A8–A10

- **Voice timing.** Run 200 warm exchanges each for private calls and groups on a controlled path. Target p95 press-to-audible-grant and release-to-next-grant of 500 ms or less, and p95 mouth-to-ear of 400 ms or less. Use synchronized acoustic measurements to assess the first word.
- **Cold starts and capacity.** Measure 50 cold starts, TURN-only paths and degraded networks. Exercise 100 connected users, ten concurrent talkgroups and one 25-member group.
- **Operations and privacy.** Finish credential refresh and replacement-handset recovery for saved groups and preferences. Verify database migrations, backup restoration, revocation and live group removal. Audit stored data and logs, then publish the deployment guide and completed gate results with the release.
