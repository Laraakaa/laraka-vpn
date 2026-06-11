# laraka-vpn — Two-Process Rewrite Plan

> Status: **PROPOSED — for review before implementation**
> Author: design synthesis from the PKCS#11 keychain spike + Oracle red-team
> Scope: replace the single root daemon with a split **user agent** + **root helper**, so the
> VPN client identity lives in the macOS login keychain (never on disk) and the menu bar
> actually draws.

---

## 0. Why this rewrite exists (the one-paragraph story)

The current daemon runs as a **root LaunchDaemon in session 0**. That single fact causes
**both** of the project's long-standing problems:

1. **The menu bar never worked.** `NSStatusItem`/systray needs a GUI (Aqua) session and a
   window server. Session 0 has neither, so the icon can never draw.
2. **The client key has to be extracted to disk.** A root process in session 0 cannot reach
   the user's `login.keychain-db` to sign (it hits `errSecInteractionNotAllowed`, -25308), so
   today the design dumps the raw private key to `/etc/vpn-cli/cert.key` and points openconnect
   at the file.

We **proved end-to-end** (see §1) that the keychain identity can drive openconnect via a
`pkcs11:` URI with the key staying `NEVER_EXTRACTABLE`. The blocker was never crypto — it is
**deployment topology**. The fix for both problems is the same: split the program into a
**user-session agent** (menu + keychain auth) and a **root helper** (routing + tunnel), and let
them talk over a Unix-domain socket carrying only an opaque session cookie.

---

## 1. What is already proven (do not re-litigate)

All of this was verified on the actual machine (Apple Silicon, `/opt/homebrew`), not assumed:

- **native-pkcs11 built + registered.** Artifact:
  `/Users/taabala3/code/personal/native-pkcs11/target/aarch64-apple-darwin/release/libnative_pkcs11.dylib`
  (1.15 MB Mach-O arm64, exports `_C_GetFunctionList` + `_C_Initialize`, links
  Security.framework + CoreFoundation). Registered at
  `/opt/homebrew/etc/pkcs11/modules/native-pkcs11.module` (`module:` / `managed: yes` /
  `critical: no`). `p11-kit list-modules` confirms it.
- **The identity enumerates as a real PKCS#11 object** through the p11-kit proxy (the same path
  GnuTLS/openconnect use — **no `--provider` flag needed**):
  - Cert: `token=Keychain;object=taabala3;type=cert`, X.509 RSA-2048, expires **2027-05-13**.
  - Key: same `CKA_ID`, flagged `CKA_PRIVATE` + `CKA_NEVER_EXTRACTABLE` + `CKA_SENSITIVE`.
- **Signing works.** `p11tool --test-sign` printed `Signing using RSA-SHA256... ok` +
  `Verifying against private key parameters... ok` (exit 0). The key signs without leaving the
  keychain.
- **openconnect works end-to-end, interactively.** Both stages passed in the user GUI session:
  - Stage 1 (`--authenticate -c 'pkcs11:...'`, no sudo): keychain signed, cookie printed.
  - Stage 2 (`sudo openconnect ... -c 'pkcs11:...' -s 'vpn-slice ...'`): full tunnel came up.
- **The cookie tunnel never touches the cert/key** (verified from openconnect v9.12 source:
  `main.c#L2059-2084`, `cstp.c#L281-352`, `gnutls.c#L939-1058`, `dtls.c#L35-39`,
  `ssl.c#L1107-1155`). `--authenticate` exits *before* `openconnect_make_cstp_connection()`; the
  tunnel CONNECT sends only `Cookie: webvpn=...` + DTLS session secrets. This is what makes the
  privilege split sound: **only the auth phase needs the keychain.**

### The authoritative identifiers (use these verbatim everywhere)

```
# PKCS#11 cert URI (id= is the authoritative selector; object=/label is only a hint)
pkcs11:model=software;manufacturer=google;serial=0000000000000000;token=Keychain;id=%A3%E4%97%32%17%CF%8E%17%19%EE%18%65%04%99%09%D1%C2%97%0B%7B

# CKA_ID (raw):  a3:e4:97:32:17:cf:8e:17:19:ee:18:65:04:99:09:d1:c2:97:0b:7b
# Cert:          CN=taabala3, RSA-2048, valid to 2027-05-13
```

Config (from `/etc/vpn-cli/vpn-daemon.yaml`, root-owned source of truth):

```
server_cert: pin-sha256:gnKCGJ6tmhP2eTEjY8ZRT1HSHMzWmAMB0K2VLxBJZVY=
slices:      138.190.0.0/16 10.89.0.0/16 10.92.0.0/16 10.144.0.0/16 10.250.0.0/16 \
             10.222.0.0/16 10.217.0.0/16 10.218.0.0/16 10.49.0.0/16
profile:     /opt/cisco/anyconnect/profile/SWISSCOM-CERTRAS_client_profile.xml
server arg:  "Swisscom Secure RAS - Mobile ID"
binaries:    /opt/homebrew/bin/openconnect, /opt/homebrew/bin/vpn-slice
```

---

## 2. Target architecture

One Go binary, three roles selected by subcommand. Two processes run as services; the CLI is
short-lived.

```
                 ┌────────────────────────────────────────────────────────────┐
   user types →  │  vpn-cli connect | disconnect | status   (CLI, short-lived)  │
                 └───────────────┬────────────────────────────────────────────-┘
                                 │  user socket (CLI ⇄ agent), 0700 dir, same-uid
                                 ▼
   ┌─────────────────────────────────────────────────────────────────────────────┐
   │  vpn-cli agent      USER LaunchAgent — Aqua session, runs as the user          │
   │  ───────────────────────────────────────────────────────────────────────────│
   │  • hosts the menu bar (systray)                                                │
   │  • ORCHESTRATOR + owner of authentication & user intent                        │
   │  • runs:  openconnect --authenticate -c 'pkcs11:...'   (keychain signs here)   │
   │    captures COOKIE + HOST + FINGERPRINT                                         │
   │  • owns the connection state machine, reauth backoff, prompt-storm cooldown    │
   └───────────────┬───────────────────────────────────────────────────────────────┘
                   │  privileged socket (agent ⇄ helper) — THE TRUST BOUNDARY
                   │  root-owned dir; peer-cred + code-signature checked
                   ▼
   ┌─────────────────────────────────────────────────────────────────────────────┐
   │  vpn-cli helper     ROOT LaunchDaemon — session 0, runs as root                │
   │  ───────────────────────────────────────────────────────────────────────────│
   │  • fixed-function tunnel supervisor (NO keychain, NO arbitrary commands)       │
   │  • on {connect, cookie, host}: builds argv from ROOT-OWNED config and runs     │
   │      openconnect --cookie-on-stdin --servercert=<config pin> ... <host>        │
   │  • owns utun + routing + child lifecycle; classifies child exits               │
   │  • reconciles orphaned tunnels on restart via a root-owned state file          │
   └───────────────┬───────────────────────────────────────────────────────────────┘
                   │  spawns + supervises
                   ▼
              openconnect (cookie mode) — owns CSTP/DTLS transient reconnect
```

**The trust boundary is the privileged socket.** Everything the helper accepts must be assumed
to potentially come from hostile same-UID code until proven otherwise (see §4).

### What crosses each boundary

| Boundary | Carries | Never carries |
|---|---|---|
| CLI ⇄ agent | `connect` / `disconnect` / `status` intent | cookies, cert material |
| agent ⇄ helper | opaque **cookie** (bytes), allowlist-validated **host** | cert/key, scripts, route lists, env, log-level, arbitrary argv |
| helper → openconnect | cookie via **stdin** (closed immediately), argv from **root config only** | cookie in argv/env |

The `--servercert` pin and the `-s 'vpn-slice ...'` route set come from **root-owned config**,
never from the agent. The agent cannot influence where root routes traffic or what it executes.

---

## 3. Process roles & responsibilities (explicit ownership)

This split is load-bearing for correctness — fuzzy ownership is how you get prompt storms and
duplicate tunnels.

- **Agent owns:** authentication, keychain prompting, cookie refresh, user intent, menu state,
  bounded reauth backoff.
- **Helper owns:** the privileged child lifecycle, route/script execution, classifying child
  exits, refusing a second concurrent tunnel.
- **openconnect owns:** CSTP/DTLS transient reconnect using the *current* cookie. The helper and
  agent must **not** fight it by reauthing on every blip.

---

## 4. The trust boundary: authenticating the agent⇄helper socket

> Oracle CRITICAL #1: `getpeereid()` authorizes a **user**, not *your agent*. Any same-UID
> process can otherwise connect to the helper and drive root-owned network state, or spam
> `disconnect` for DoS. `getpeereid()` is necessary but **not sufficient.**

### Layered peer authentication (defense in depth), in order

1. **`getpeereid()`** → require the peer's UID == the configured allowed UID; **reject uid 0**
   and any non-configured user.
2. **Peer PID** via `getsockopt(LOCAL_PEERPID)`.
3. **Immediately** validate that PID with Security.framework `SecCode` / `SecRequirement`
   against a **hardcoded designated requirement** for the signed agent. (PID-based checks are
   race-prone — this is weaker than an audit token, accepted as a documented limitation.)
4. **Socket hygiene:** the privileged socket lives in a **root-owned, non-world-writable**
   directory (`/var/run/laraka-vpn`, mode `0755` root:wheel, socket `0600`).
5. **Low-power protocol:** the helper accepts only `connect(cookie, host)`, `disconnect`,
   `status`. No arbitrary args, no script path, no route list, no env, no config override, no
   log-level knob that could leak secrets. Cap message size; never log raw frames.

> **Stretch / strongest option (deferred, not v1):** move the privileged boundary to a launchd
> **Mach service / XPC** and validate the caller's **audit token** against the agent's
> `SecRequirement`. This is the Apple-blessed way and removes the PID race entirely. Tracked as a
> follow-up (Oracle estimates +1–2 days). v1 ships the hardened-Unix-socket version above.

A bare nonce does **not** help (same-UID malware can read it). The only real defense is code
identity (PID→SecCode now, audit-token→XPC later) plus a deliberately low-power protocol.

---

## 5. The keychain ACL story (corrected — drop `unsigned:`)

> Oracle CRITICAL #2: **RETRACT** the earlier `security set-key-partition-list ... unsigned:`
> one-liner. `unsigned:` likely lets **any** unsigned same-user code use the non-extractable key
> as a **signing oracle** (key stays non-extractable but remains *usable*). And note: the
> keychain ACL governs **the process that performs the keychain op = the `openconnect` binary
> loading native-pkcs11**, *not* the Go agent. Signing the Go binary is irrelevant to key use.

### Plan

1. **Sign the binary that actually calls Security.framework** — i.e. `openconnect` (or a stable
   signed wrapper that is the process invoking it), with a **stable designated requirement**
   (Developer ID or a stable local identity, fixed bundle id). Avoid ad-hoc signatures that
   change across rebuilds and break ACL persistence.
2. **Grant the keychain ACL to that requirement**, not to `unsigned:`.
3. **Verify on the deployment target** whether loaded dylibs influence the assessed code
   identity. If Keychain evaluates only the main executable, we must sign `openconnect` itself
   (or the wrapper that is the main executable), because the dylib (`native-pkcs11`) won't carry
   the identity.
4. **Interactive first run is acceptable for v1:** the user approves the keychain prompt once
   ("Always Allow") from the GUI session. The agent (Aqua) is the right place for that prompt to
   appear. Unattended-from-cold-boot signing is a hardening follow-up, gated on step 3's finding.

This is the one area with the most version-specific macOS sharp edges — **must be validated on
the actual target before declaring done.**

---

## 6. Cookie hygiene (it's a bearer token)

> Oracle HIGH #3. `--cookie-on-stdin` is correct (not in `ps`; macOS has no Linux `/proc/<pid>/fd`
> by default). The real risks are **in-memory copies** and **logging**.

Rules:

- Send the cookie as **bytes**, not a long-lived `string`. Avoid gratuitous copies through JSON
  buffers where practical.
- **Never** put it in argv, env, logs, metrics, crash reports, panic output, or state files.
- Helper writes it to the child's **stdin, then closes stdin**, then zeroes the byte slice
  best-effort. The helper does **not** retain the cookie after spawn.
- Let `openconnect` retain it for its own reconnect (verified from source).
- On `disconnect`, **kill the openconnect child** so its retained cookie dies with it.
- Accept the irreducible truth: you **cannot** hide the cookie from root or from a compromised
  `openconnect`.

---

## 7. Connection state machine (agent-owned)

> Oracle HIGH #5. Explicit states + explicit retry rules prevent prompt storms and reauth loops.

States: `Idle` → `Authenticating` → `AuthFailed` | `Connecting` → `Connected` →
`Degraded/Reconnecting` | `SessionRejected` | `Disconnected` | `Unknown`.

Retry rules:

- **Degraded/Reconnecting** → let **openconnect self-heal**. Do **not** reauth.
- **SessionRejected** (cookie/auth rejected, classified from child output/exit) → agent reauths
  with **bounded exponential backoff**.
- **AuthFailed from keychain** (e.g. `errSecInteractionNotAllowed`, user cancel) → **STOP**,
  surface a user action; do **not** loop. Message: *"Keychain access not authorized; open once
  from the GUI and approve."*
- **sleep/wake** → wait for the helper's classification before reauthing (network is stale).
- **Unknown** → disconnect/cleanup, then reconnect only if user intent is still "connected."

Prompt-storm mitigation: only **one** auth attempt in flight; cooldown after
cancel/`-25308`/repeated failures; a max retry budget per wake/network-change window.

---

## 8. Helper lifecycle & restart reconciliation

> Oracle HIGH #4. The daemon lifecycle ≠ the child (tunnel) lifecycle. A naive `KeepAlive` plus
> dumb startup turns a crash into a tunnel-spawn loop, or orphans/duplicates tunnels.

- Helper keeps a **root-owned runtime state file** (child PID, start time, exe path, current
  state) in `/var/run/laraka-vpn/`.
- On startup, **reconcile**: if the recorded child is *definitely* its openconnect, adopt or
  kill it; if **uncertain, fail closed** (prefer "disconnected, agent must reconnect" over "maybe
  two tunnels").
- **Refuse a second tunnel** while one is active or in `Unknown`.
- Planned shutdown: **SIGTERM the process group**, then **SIGKILL after a timeout**.
- `KeepAlive` in the helper plist means **keep the IPC endpoint available**, *not* "resurrect the
  tunnel." The tunnel is kept alive by openconnect first, then by agent-driven reauth only if the
  session is truly dead.

---

## 9. macOS session model & failure paths

> Oracle MEDIUM-HIGH #6. A LaunchAgent in **Aqua** is correct for menu bar + login keychain on a
> graphical login. But "console user" is not always the right predicate.

- Agent plist **must** set `LimitLoadToSessionType = Aqua` + `RunAtLoad`. Do **not** run the menu
  agent from a LaunchDaemon or non-Aqua session.
- **Authorize a configured UID at install time** (single human account), not "whoever is at the
  console" — robust against fast-user-switching.
- **Fail cleanly** when there's no Aqua session (SSH-only/headless, locked keychain, loginwindow):
  `vpn-cli connect` must print a precise error — *"No Aqua agent/keychain session available; log
  in graphically first"* — and exit non-zero. We do **not** promise headless connect in v1.

---

## 10. Remaining hardening (root attack surface)

- **§10a Socket TOCTOU (Oracle MEDIUM #7):** `/var/run/laraka-vpn` created root:wheel, not
  group/other writable; remove stale sockets only after verifying the path is a socket inside the
  root-owned dir; never follow symlinks; set `umask` before bind then `chmod` the socket; a
  launchd singleton / lock file so two helpers can't race-bind; cap NDJSON frame size; read/write
  deadlines so a client can't connect-and-stall. User socket under a per-user dir mode `0700`.
- **§10b Host validation (Oracle MEDIUM #8):** the cookie-auth gateway host **may differ** from
  the profile host (load balancer) — do **not** hardcode one host. Root config holds an
  **allowlist** of exact hosts or tightly-scoped suffixes. Normalize before match (lowercase,
  trim trailing dot, reject NUL/control chars, userinfo, paths, ports unless allowed; parse as
  hostname not URL; reject IP literals unless configured). The `--servercert` PIN always comes
  from root config.
- **§10c Exec hardening (Oracle MEDIUM #9):** absolute paths for `openconnect`/`vpn-slice`/any
  interpreter (no `PATH` resolution); root-owned non-writable exe paths; minimal child env; no
  shell invocation; keep the `-s 'vpn-slice ...'` string entirely from root config and validate
  at load; validate config file perms (root-owned, not group/world writable).

---

## 11. Code plan — salvage 3 organs, rewrite the skeleton

> User decision: *"Rewrite skeleton, salvage 3 organs."* New topology + CLI wiring; transplant
> verbatim (with noted fixes) the three pieces below. Drop ZeroMQ (`pebbe/zmq4`) for
> Unix-domain sockets.

### Salvage organ A — openconnect stdout scan (the only Swisscom-specific runtime knowledge)

From `internal/daemon.go` L182/L187. **Moves into the ROOT HELPER** (which supervises the
cookie-mode child), not the agent:

```go
successRe := regexp.MustCompile(`Configured as (\d+\.\d+\.\d+\.\d+), with SSL connected and DTLS connected`)
failureRe := regexp.MustCompile(`Failed to reconnect to host ([a-zA-Z0-9.-]+): Can't assign requested address`)
```

Keep the `bufio.Scanner`-over-stdout success/failure detection loop. The helper uses it to drive
`Connecting → Connected` and to classify failures for the agent.

### Salvage organ B — systray menu (`internal/menu.go`)

**Moves into the user AGENT.** Fix the existing bugs during transplant:
- De-duplicate the **two Quit items**; build all menu items in `onReady` (not inside a
  goroutine — the current code is race-prone).
- Wire `mConnect`/`mDisconnect` to **send IPC to the agent's orchestrator** (which talks to the
  helper), instead of calling `d.Connect()`/`d.Disconnect()` directly.
- Icon stays `internal/icon.Data` (template icon). It only failed before because it ran in
  session 0 — in the Aqua agent it will draw.

### Salvage organ C — plumbing

cobra CLI shape + `utils.Logger` (zap, `utils/logger.go`, 17 lines, keep as-is) + viper config
loading. The CLI's `--address` flag changes from `tcp://127.0.0.1:7770` to a **Unix socket
path**.

### Throw away

Single-process/root-does-everything topology; the `--sslkey`/`--certificate` on-disk file auth
(`daemon.go` L148-149); the menu+daemon+root conflation in `cmd/start.go`; ZeroMQ.

### Proposed package layout (for review)

```
cmd/
  root.go        # cobra root; subcommands: agent, helper, connect, disconnect, status
  agent.go       # `vpn-cli agent`  — Aqua LaunchAgent entrypoint (menu + auth orchestrator)
  helper.go      # `vpn-cli helper` — root LaunchDaemon entrypoint (tunnel supervisor)
  connect.go     # `vpn-cli connect`    — CLI → agent
  disconnect.go  # `vpn-cli disconnect` — CLI → agent
  status.go      # `vpn-cli status`     — CLI → agent
internal/
  agent/         # orchestrator, state machine (§7), openconnect --authenticate runner, cookie capture
  helper/        # privileged supervisor, child lifecycle (§8), stdout scan (organ A), reconcile
  ipc/           # Unix-socket framing (NDJSON, size cap, deadlines) + peer auth (§4)
  menu/          # systray (organ B), agent-only
  config/        # viper loaders; root config vs user prefs; host allowlist (§10b)
  icon/          # unchanged
utils/           # logger.go (organ C), unchanged
install/
  ninja.lara.vpncli.helper.plist   # root LaunchDaemon → `vpn-cli helper`
  ninja.lara.vpncli.agent.plist    # user LaunchAgent (LimitLoadToSessionType=Aqua) → `vpn-cli agent`
```

`install/ninja.lara.vpncli.plist` (current root daemon, `KeepAlive true`, no `UserName`, runs
`vpn-cli start`) is **replaced** by the two plists above.

---

## 12. The openconnect invocations (verbatim, for implementation)

**Agent (auth phase, user session, keychain signs):**

```
openconnect --protocol=anyconnect --os=mac-intel \
  --xmlconfig=/opt/cisco/anyconnect/profile/SWISSCOM-CERTRAS_client_profile.xml \
  --servercert=pin-sha256:gnKCGJ6tmhP2eTEjY8ZRT1HSHMzWmAMB0K2VLxBJZVY= \
  -c 'pkcs11:model=software;manufacturer=google;serial=0000000000000000;token=Keychain;id=%A3%E4%97%32%17%CF%8E%17%19%EE%18%65%04%99%09%D1%C2%97%0B%7B' \
  --authenticate --non-inter \
  'Swisscom Secure RAS - Mobile ID'
# → captures COOKIE=..., HOST=..., FINGERPRINT=...   (v9.12 infers -k from -c; -c alone is enough)
```

**Helper (tunnel phase, root, cookie only — never touches the keychain):**

```
openconnect --protocol=anyconnect --cookie-on-stdin \
  --servercert=pin-sha256:gnKCGJ6tmhP2eTEjY8ZRT1HSHMzWmAMB0K2VLxBJZVY= \
  -s 'vpn-slice 138.190.0.0/16 10.89.0.0/16 10.92.0.0/16 10.144.0.0/16 10.250.0.0/16 10.222.0.0/16 10.217.0.0/16 10.218.0.0/16 10.49.0.0/16' \
  <validated-host>
# cookie arrives on stdin; stdin closed immediately after write
```

Note the `-s` value is split correctly here (`vpn-slice` + subnets), sidestepping the
pre-existing `daemon.go` L151 bug — but per scope we are **not** "fixing" the old code, we are
writing new code that is correct from the start.

---

## 13. Two standing workstreams (independent of this rewrite)

- **(a) Cert expiry — hard deadline.** The on-disk `cert.pem`/`cert.key` the daemon uses *today*
  (SHA-1 `2D:C7:6A:F4…`) **expires 2026-05-08**. The keychain identity `taabala3`
  (SHA-1 `34:97:97:40…`) is valid to **2027-05-13**. This migration *is* the thing that moves the
  loop onto the 2027 identity — landing it before the 2026 deadline is the real urgency driver.
- **(b) Goal framing.** The daemon **already** runs on an on-disk key at `/etc/vpn-cli/cert.key`,
  so "never write the raw key to disk" is a **new posture**, not the current state. The existing
  on-disk key is already exposed; **rotating it out / deleting it** is a *separate cleanup
  workstream* once the keychain path is live. (Decide: is the primary goal "improve from here"
  or "the key must never be exposed"? The rewrite delivers the former; the latter additionally
  requires the cleanup.)

---

## 14. Build order (once this plan is approved)

0. **Establish a clean baseline** — commit/branch the current uncommitted changes first
   (`Makefile`, `cmd/*`, `internal/*`, `go.mod/sum` are all dirty) so the rewrite starts from a
   known point.
1. `internal/ipc` — Unix-socket framing + peer auth (§4). Unit-test peer-cred rejection.
2. `internal/helper` — supervisor + stdout scan (organ A) + reconcile (§8). Test against the
   verbatim Stage-2 invocation (§12) with a cookie captured by hand.
3. `internal/agent` — auth runner + cookie capture + state machine (§7).
4. `internal/menu` — transplant + de-bug organ B; wire to agent.
5. CLI subcommands + the two launchd plists (§11).
6. Keychain ACL / code-signing validation on the real target (§5) — **the riskiest unknown**;
   do it early enough that it can't ambush the end.
7. End-to-end: install both services, connect via menu, verify tunnel, verify reconnect after
   sleep/wake, verify clean failure with no Aqua session.

---

## 15. Open questions for the reviewer (you)

1. **v1 trust boundary:** ship the hardened **Unix socket** (PID→SecCode, §4) and treat XPC/Mach
   audit-token as a fast-follow? Or hold v1 until the XPC boundary is in?
2. **Keychain ACL:** are you willing to **sign `openconnect`** (or a stable wrapper) with a
   stable identity (§5)? This is required to ever go fully unattended; v1 can rely on a one-time
   GUI "Always Allow" instead.
3. **Goal (§13b):** is the objective "improve from here" (rewrite only) or "key must never be
   exposed" (rewrite **+** rotate out the existing on-disk key)?
4. **Headless:** confirm we can drop SSH-only/headless connect for v1 (fail cleanly), since
   keychain signing needs an Aqua session.
