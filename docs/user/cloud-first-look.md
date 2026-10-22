# See Sessions in Cloud from a browser or phone

Use this walkthrough after [your first local Session](first-session.md) is visible. Keep Clawdline running on the machine you want to reach. You will sign in to your account, connect that machine, then pair each browser or installed web app you want to use.

## 1. Sign in and connect the machine

On macOS, open Clawdline and select its **Cloud** tab. Choose **Continue with GitHub**. Sign in in your default browser and allow it to open Clawdline when asked. Return to the app's Cloud tab: it should show your account or machine list.

If this machine is not connected yet, choose **Connect this machine**. Approve the native confirmation, complete the machine approval in your default browser with the same GitHub account, then return to Clawdline. Account sign-in and machine approval are separate: the first identifies you; the second registers this computer and connects it to Cloud.

Choose **Pair this app**, then **Pair directly with this app's machine**. Approve its native confirmation, compare the fingerprints, and choose **They match — done**. **Success in the app:** this machine's Sessions appear in the Cloud tab. If this machine is already connected or the app is already paired, skip the completed step. For a machine without the app, use [the terminal connection steps](remote-access.md#connect-the-machine).

## 2. Pair a new browser on this computer

1. Open [app.clawdline.com](https://app.clawdline.com/) in the browser and profile you plan to use. Sign in to the same GitHub account.
2. Open **Pair**, then choose **Show the pairing code**. You can start before selecting a machine; the app checks that the code reaches this computer.
3. Choose **Open Clawdline to pair**. Allow the browser to open the installed app, check the machine named in its confirmation, then choose **Pair with this machine**. Keep the browser's pairing page open.
4. Return to that browser. Compare the fingerprints shown by the pairing flow, then choose **They match — done**. The machine's Sessions should appear.

The app reports **Browser paired** when its local handover finishes. The browser must also finish and show Sessions. If the app does not open, use [the code or command fallback](remote-access.md#pair-with-a-code-or-command). The app's Cloud tab and every browser profile have separate pairing keys.

## 3. Pair your phone

On the computer, open the app's **Cloud** tab and find **Pair a browser or phone**. If the Session list already appears, open **Settings → Workflow and control → Pair a browser or phone**. Choose **Show a pairing QR code** and approve the native confirmation. Leave the QR visible; click it to enlarge it.

- **iPhone or iPad:** open app.clawdline.com in Safari, use **Share → Add to Home Screen**, then open Clawdline from its Home Screen icon. Sign in there. Choose **Pair → Scan the machine's QR** inside that app and allow the camera. Do this before pairing: Safari and the Home Screen app store separate keys.
- **Android:** open app.clawdline.com in Chrome and sign in. If you want an installed app, install it from Chrome's menu first. In the browser profile or installed app you will keep using, choose **Pair → Scan the machine's QR** and allow the camera.

On either phone, scan the computer's QR, compare the fingerprints, then choose **They match — done**. **Success on the phone:** the selected machine's Sessions appear. **Success on the computer:** the pairing block reports **Browser paired**. If live scanning fails, use **Black screen? Choose a photo of the QR** or **Choose a QR image** inside the same phone app; images are processed there. See [phone recovery](remote-access.md#pair-an-iphone-ipad-or-android-phone) for expired codes and camera permissions.

These App shortcuts require a Clawdline app version that supports them. Updating only the hosted website does not update the native app. On a machine without the app, use the [terminal pairing path](remote-access.md#pair-with-a-code-or-command).

## Read, act, and reconnect

Pairing supplies a browser's keys. Sending also requires this machine's Cloud command switch and the browser's local grant to allow operation; see [Let paired devices act](remote-access.md#let-paired-devices-act).

If the machine is offline, Cloud may retain an older view. Restore its connection before treating the view as fresh. If the machine is visible but this browser still asks to pair, check the GitHub account, browser profile, and whether you opened Safari or the Home Screen app.

## Use Sessions from another local machine

The CLI uses a separate Cloud viewer identity. On the machine where you will read, run `clawdline cloud viewer login`, approve the displayed device and capabilities in your Cloud account, then run `clawdline cloud viewer machines`. For each target machine, compare its fingerprint on that machine before running `clawdline cloud viewer pair --machine MACHINE_ID --fingerprint FINGERPRINT`. Run the printed one-time `clawdline cloud pair --offer` command on the target machine. `clawdline cloud viewer sessions` lists paired machines and their Session status. To use a browser, pair that browser separately; the hosted Console shows machines in its existing Session list.

An actionable Session has three values: machine ID, Session ID, and execution generation. Select the current row in the hosted Console or pass all three to `clawdline cloud viewer read --machine MACHINE_ID --session SESSION_ID --generation GENERATION`. The first read shows the newest transcript page. If it returns `next_before`, use `clawdline cloud viewer read --machine MACHINE_ID --session SESSION_ID --generation GENERATION --before NEXT_BEFORE` to fetch one older page. Repeat with the new cursor until none remains. Each page checks the same current execution and permission again. `send`, `answer`, `interrupt`, and `end` use the same target. A CLI action prints its request ID before sending. If its outcome is uncertain, query `clawdline cloud viewer receipt --machine MACHINE_ID --session SESSION_ID --generation GENERATION --action ACTION --request REQUEST_ID` before acting again. An offline or stale machine does not queue an action.

[Pairing, permissions, and revocation](remote-access.md) · [Cloud trust boundary](../cloud.md) · [Agent contract for Cloud actions](../agent-contract.md)
