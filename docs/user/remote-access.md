# Remote access: another browser, a phone, Clawdline Cloud

After this page you can open the console somewhere other than the machine's own browser — another
computer, or your phone — pair that device to read only or to read and operate, and revoke it when needed.
Your agents and code stay on your machine whichever way you choose.

## Which way to choose

| Way | Account | Reaches | What it can do | State |
| --- | --- | --- | --- | --- |
| A. SSH port forward | None | Another computer you can SSH from | Read and operate unless set to read only | Works |
| B. Tailscale Serve | A Tailscale account, signed in on both ends | Devices on your own tailnet, including a phone | Read only or read and operate, chosen per device after pairing | Built; verified over a real tailnet |
| C. Clawdline Cloud | Clawdline account (GitHub sign-in) | Any browser, including a phone; several machines | Read and act on paired machines while their Cloud commands are on | Preview |

The daemon itself listens on `127.0.0.1` only. None of these opens a port on your network.
A device paired directly with this machine is either **read only** or **read and operate**, chosen
per device on this machine (see [Read only or read and operate](#read-only-or-read-and-operate)).
Clawdline Cloud has a separate switch for the whole machine and a local grant for each pinned
browser (C below). Both must allow operation before that browser can act.

Clawdline no longer runs a Cloudflare tunnel of its own (removed on 2026-10-20; the automatically
generated `trycloudflare.com` address went the day before). A settings file that still holds the
old tunnel settings (`remote_tunnel`, `remote_tunnel_name`, `remote_hostname`, `cloudflared_path`)
starts nothing, and that tunnel's hostname is no longer an address the daemon answers. Use B or C instead.

Labels below follow the English interface; the console also supports other languages.

## A. Another computer, over SSH

On the computer you are sitting at, forward the port and ask the machine for a sign-in address:

```sh
ssh -L 7727:127.0.0.1:7727 you@your-machine
./bin/clawdline open --print          # in that SSH session; the browser can read and type
```

Open the printed address in your local browser. It carries a key in its fragment; treat it like a
password until it has been used.

**Check:** the console loads with the machine's sessions. **Undo:** stop the SSH forward, and
revoke the browser (see [Signed-in devices](#see-and-revoke-devices-signed-in-directly)): each
`clawdline open` creates a device of its own, and it stays signed in until it is revoked.

## B. A phone on your tailnet, through Tailscale Serve

Verified on 2026-10-20 over a real tailnet, with Serve's own certificate: the address opens the
pairing page, an unpaired client is refused Sessions, a paired client reads and sends, and revoking
it refuses its next request. The client was a Tailscale node, not yet a phone app.

[Tailscale Serve](https://tailscale.com/kb/1312/serve) gives this machine an HTTPS address that only
devices on your own tailnet can open, and passes each request to the daemon on `127.0.0.1:7727`.
Clawdline does not sign anybody in with their Tailscale identity and does not manage Tailscale
accounts or keys: a phone that reaches the address still pairs with a six-digit code.

1. **Install Tailscale and sign in on both ends**, this machine and the phone, with the same
   tailnet.
2. **In the Tailscale admin console, turn on MagicDNS and HTTPS certificates** (DNS page). Serve
   needs both to answer on `https://<machine>.<tailnet>.ts.net`.
3. **On this machine, start Serve in the background:**

   ```sh
   tailscale serve --bg 7727
   tailscale serve status        # prints the https://<machine>.<tailnet>.ts.net address
   ```

4. **Tell Clawdline that address.** Turn on **Let a browser or your phone view your sessions**, and
   enter the full name, without `https://`, in **Tailscale address** on the same tab. Without the
   app, set both in `config.json` in the state directory and restart `serve`:

   ```json
   { "remote": true, "tailscale_hostname": "my-machine.your-tailnet.ts.net" }
   ```

   The daemon answers that name only while both are set. Turning the switch off, or clearing the
   name, refuses the address on its next request even while Serve still runs.
5. **Pair the phone**: keep `./bin/clawdline pair --watch` running on this machine (or keep the
   macOS app open), open the address on the phone, name the device, press **Ask to pair**, and type
   the six-digit code that appears on this machine — in `pair --watch`, or as an alert in the macOS
   app, and never on the phone — within two minutes.

**Serve, not Funnel.** Serve is private to your tailnet. `tailscale funnel` publishes the same
address to the whole internet; it is not recommended, and nothing in Clawdline needs it.

**Check:** the phone shows **Paired — this browser is in** and then the session list.

**Limits.** Asking to pair is limited to three requests every ten minutes, and five wrong codes end
the attempt.

**Undo:** `tailscale serve --https=443 off` (or `tailscale serve reset`) stops Serve; clear
**Tailscale address**; revoke the phone (see
[Signed-in devices](#see-and-revoke-devices-signed-in-directly)).

### Read only or read and operate

Each device paired with a code, signed in with the password, or opened with `clawdline open` has
one of two grants:

- **Read only** — it sees Sessions, terminal screens, and the documents and images Clawdline shows.
  It cannot send, type in a terminal, or start or close anything. Notifications still reach it.
- **Read and operate** — it can also send messages, type in terminals, and start or close Sessions.
  An assistant can run programs on this machine because of what it sends.

Only this machine chooses. On the **Devices** page, under **Signed in to this machine**, each
device shows its grant and offers the other one; the page reads the list again before it shows
the change. From a terminal on the machine:

```sh
./bin/clawdline devices permission <device-id> read-only           # or read-and-operate
./bin/clawdline pair --permission read-only                        # choose before showing the code
./bin/clawdline devices pairing-permission <pairing-id> read-only  # a pairing still waiting for its code
```

A device paired before this choice existed, and a new pairing the machine did not set, stays
read and operate. A device cannot raise its own grant: the pairing request and its confirmation
ignore any permission they carry. Clawdline Cloud has its own machine-wide command switch (below),
which does not change these grants.

### See and revoke devices signed in directly

Browsers signed in with `clawdline open`, devices paired with a code, and devices signed in with the
password are listed in two places. Only this machine's own key can see the list or revoke from it:

- **The macOS app's window**, on the **Devices** page, under **Signed in to this machine**. Each device shows what it may do, when it signed in and when it was last used.
  **Revoke** asks first, naming the device, and only **Revoke it** acts. The line
  **This window uses the machine's own key** says the window itself uses the machine's own key: that key is
  never in the list and cannot be revoked there.
- **A terminal on the machine:**

  ```sh
  ./bin/clawdline devices                    # one line per device: id, capabilities, signed in, last used, name
  ./bin/clawdline devices revoke <device-id> # names the device and asks [y/N]; --yes skips the question
  ```

A browser signed in as a device of its own is not shown the list. Its Devices page says **This browser is signed in as its own device** and offers **Sign this browser out**, which asks first and then revokes that browser's own key.

**Check:** a revoked device's next request is refused and it is back at the pairing page.
Revoking does not take back what the device has already seen or downloaded.

These are not the browsers Clawdline Cloud relays for; those are `clawdline cloud devices` (C below).

## C. Clawdline Cloud

Clawdline Cloud is off by default. When on, the machine keeps an outbound connection to
app.clawdline.com that carries only end-to-end encrypted envelopes: the service passes them along
and cannot read your sessions. Cloud is also how you use **several machines** from one console.

Cloud is a preview. The App shortcuts below require an app build that includes the matching native pairing support; a hosted-console update alone does not supply it.

### Connect the machine

On macOS, use Clawdline's **Cloud** tab:

1. Choose **Continue with GitHub**. Complete sign-in in your default browser and allow it to open Clawdline. Return to the Cloud tab and check that your account appears.
2. If this machine is not connected yet, choose **Connect this machine**. Approve the native confirmation, then approve the machine in your default browser with the same GitHub account. Return to Clawdline after approval.
3. Choose **Pair this app**, then **Pair directly with this app's machine**. Approve the native confirmation, compare the fingerprints, and choose **They match — done**. Check that this machine's Sessions appear in the Cloud tab. Skip a step that is already complete.

Account sign-in, machine registration, and browser pairing are separate. Signing in proves which account you own; the local connection step registers this computer and starts its Cloud connection; pairing gives this app or browser the keys to read Sessions. Another browser must pair separately.

If sign-in does not return to Clawdline, allow the browser's request to open the app, or open Clawdline and try again. Install an updated native app if the connection or pairing controls are missing; updating the website alone does not update the app.

For a machine without the app, run these commands on the target machine through a terminal or SSH:

```sh
clawdline cloud login
clawdline cloud on
clawdline cloud status
```

`cloud login` prints an address and a code. Open the address, choose **Continue with GitHub**, sign in to the same account, and approve the machine within ten minutes. Then `cloud on` turns on the running daemon's Cloud connection without a manual restart. `cloud status` shows the identity, switch, and connection. Check that the selected machine is connected in Cloud before pairing a browser.

If the shell says `command not found: clawdline`, open a new terminal after installation. With a standard installation you can also run the executable at `~/.local/bin/clawdline` in place of `clawdline`. For a custom install, use that installation's executable. Never register a different machine as a substitute.

### Pair a browser on the same computer

1. In the browser and profile you want to use, open [app.clawdline.com](https://app.clawdline.com/) and sign in to the same account.
2. Open **Pair**, then choose **Show the pairing code**. You do not need to select a machine first; the app checks that the code reaches this computer.
3. Choose **Open Clawdline to pair**. Allow the browser to open the app. Verify the machine named in the native confirmation and choose **Pair with this machine**.
4. Keep the browser page open. The app reports **Browser paired** after its handover. Return to the browser, compare the fingerprints, and choose **They match — done**. Its Session list is the browser's success check.

This shortcut requires the updated macOS app. If the app does not open or the button is unavailable, use [the code or command fallback](#pair-with-a-code-or-command). A browser on another computer cannot use this shortcut to pair a remote machine.

### Pair an iPhone, iPad, or Android phone

First show a QR on the computer: in the macOS app's **Cloud** tab, find **Pair a browser or phone**. If the Session list already appears, open **Settings → Workflow and control → Pair a browser or phone**. Choose **Show a pairing QR code**, approve the native confirmation, and leave the code visible. Click the QR to enlarge it. The QR expires after about ten minutes; **Show a pairing QR code** or **Get a new QR code** creates a fresh invitation after confirmation.

**iPhone or iPad:**

1. Open app.clawdline.com in Safari and use **Share → Add to Home Screen**. Enable **Open as Web App** if offered, then add it.
2. Open Clawdline from the Home Screen and sign in to the same account. Pair inside this app; Safari has separate keys.
3. Choose **Pair → Scan the machine's QR** inside Clawdline, allow the camera, and point it at the computer's QR. Scanning with the system Camera can open Safari instead.
4. Compare the fingerprints and choose **They match — done**. Check that this app shows the machine's Sessions.

**Android:**

1. Open app.clawdline.com in Chrome and sign in to the same account. If you want the installed web app, install it from Chrome's menu before pairing.
2. In that browser profile or installed app, choose **Pair → Scan the machine's QR**, allow the camera, and scan the computer's code.
3. Compare the fingerprints and choose **They match — done**. Check that the machine's Sessions appear; keep using this same profile or app.

The computer's pairing block reports **Browser paired** when the exchange finishes. On either phone, a denied camera permission can be enabled in the device's browser/app permissions. You can also use **Black screen? Choose a photo of the QR** or **Choose a QR image** inside the pairing page; the image is processed locally in that browser/app, and still needs a readable, unexpired Clawdline invitation. A photo is a scanning fallback, not a transfer of an existing browser's keys. If neither works, use the code path below.

### Pair with a code or command

Use these recoveries if the app does not open, live scanning fails, or the target machine is headless:

- **Paste a code locally:** in the browser or phone to pair, choose **Pair → Show the pairing code** (or **Can't scan? Use a pairing code instead**; after a camera error, choose **Use a pairing code**). Copy the entire code. On the target computer, open the macOS app's **Cloud** tab → **Settings → Workflow and control → Pair a browser or phone**, paste it into **Paste the pairing code from the browser you want to connect**, choose **Pair this browser**, and approve the native confirmation.
- **Use a terminal on the target machine:** on the browser's pairing card, choose **Copy pairing command** and run that complete command once on the target machine. Its executable lookup also checks installed locations when `clawdline` is not on PATH. If it reports **Clawdline was not found**, install and start Clawdline on that machine, then generate a new code. Do not copy only the code into the shell.
- **Show a QR without the app:** run `clawdline cloud pair` on the target machine. It prints a QR, a one-time link, the fingerprints, and the expiry. Scan inside the phone app you intend to use. A desktop browser can open the printed link in its intended profile.

Keep the original browser page open until it finishes. Compare the machine fingerprint in the browser with the machine's fingerprint and, when printed, the browser fingerprint with the machine's `browser` line. If they differ, reject/remove that pairing. These paths still need explicit access to the target machine; signing in to the hosted website alone cannot authorize the handover.

An expired invitation must be replaced on the machine. An expired browser code can be replaced with **New code** in that browser; carry the new code again. If you see **wrong_account**, sign in to the same account on both ends. Pair each browser profile, private window, and installed app separately; clearing its site storage removes its keys.

### Let paired devices act

A paired browser can send text, answer, and start or close Sessions only when both its local
`operate` grant and this machine's Cloud command switch allow it:

```sh
./bin/clawdline cloud commands on     # takes effect on the next request; `off` stops it at once
```

Choose a browser's grant on the machine with `clawdline cloud grant <device-id> readonly` or
`operate`. Use `clawdline cloud pair --scope readonly` to pair a read-only browser. Omitting scope
when pairing again preserves its existing grant; a never-pinned browser keeps the legacy
`operate` default. A remote browser cannot promote itself. See [per-browser machine grants](../cloud.md#per-browser-machine-grants).

### See and remove devices

```sh
./bin/clawdline cloud devices
./bin/clawdline cloud revoke <device-id>
./bin/clawdline cloud off             # the running daemon disconnects; no restart
```

### Plans

Clawdline Cloud has a Free plan and a Pro plan ($10 a month); the **Plan** page of the
hosted console shows your plan and its limits. Everything on this machine without Cloud stays free
and needs no account.

## Troubleshooting

- **The Tailscale address answers "Wrong host."**: the name in **Tailscale address** does not
  match the one `tailscale serve status` prints, or remote access is off.
- **The old Cloudflare address no longer opens**: Clawdline stopped running that tunnel; use
  Tailscale Serve (B) or Clawdline Cloud (C).
- **The Cloud line stays off**: `./bin/clawdline cloud status` says why. A broken Cloud setting
  never stops the daemon.
- **A phone can read but not act**: check that it is paired with the selected machine and that
  `clawdline cloud commands` is on there and its local grant is `operate`. Over Tailscale Serve, check that its sign-in is still valid.
- **Notifications on the phone**: [notifications.md](notifications.md).

## Deeper

- [getting-started.md §6](../getting-started.md#6-reach-it-from-a-phone) — the same Cloud steps in
  the longer guide.
- [remote.md](../remote.md) — the line between the free product and Cloud (in Chinese).
- [cloud-wire.md](../cloud-wire.md) — envelopes, keys, pairing and signed commands (in Chinese).
- [projects.md](projects.md) — bringing project settings to a second machine.
