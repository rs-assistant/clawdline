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
starts nothing, and that tunnel's hostname is no longer an address the daemon answers. The
**Remote** tab says so once; **Got it** removes those settings from the file. Use B or C instead.

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

Cloud is a preview. The machine side has been tested against a local copy of the service.

### Connect the machine

```sh
./bin/clawdline cloud login           # prints a code and an address; approve this machine there
./bin/clawdline cloud on              # turn the line on
# restart `serve`: the daemon reads the Cloud switch when it starts
./bin/clawdline cloud status          # the switch, the identity, the connection
```

Sign in at app.clawdline.com with **Continue with GitHub**. `cloud login` waits up
to ten minutes for you to approve the machine.

### Pair a browser or a phone

Any one of these:

- **From the machine:** `./bin/clawdline cloud pair` prints a one-time link. Open it in a browser
  signed in to the same account.
- **From the macOS app:** its settings window has **Pair browser** → **Create pairing QR code**. **On an iPhone or iPad, first add app.clawdline.com to the Home Screen
  (Safari → Share → Add to Home Screen), open it from there, sign in, and scan from inside it.** A
  pairing made in Safari does not carry over to the Home Screen app.
- **From the browser, using a paired helper machine:** in the hosted console, press
  **Pair** on the target machine's row, choose a machine this browser is already
  paired with, then start its pairing assistant. The helper uses access it already
  has to reach the target. The card reports progress and the fingerprints to compare.
  The helper must be online and its Cloud command switch must be on.
- **From the browser, manually:** the same card shows
  `clawdline cloud pair --offer <code>` to run on the target machine if a helper is
  unavailable or cannot reach it.

Both screens show the same fingerprint when pairing finishes; compare them.

**Check:** app.clawdline.com lists the machine with its sessions.

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
./bin/clawdline cloud off             # and restart `serve`: the line goes down
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
