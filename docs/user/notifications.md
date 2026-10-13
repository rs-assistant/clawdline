# Notifications

This page lists the notifications the current Go daemon can send. A browser can
also send a test notification to its own device.

## How it works

Notifications are standard Web Push. The daemon on your machine sends them itself, over outbound
HTTPS to your browser's push service; no Clawdline account is needed. Each browser or phone that
should be told subscribes once.

## Before you start

- A browser can subscribe only on a secure page: `http://127.0.0.1:7727` on the machine itself,
  your own tunnel's HTTPS address, or the hosted console at app.clawdline.com
  ([remote-access.md](remote-access.md)). An address on your local network (`http://192.168…`)
  cannot.
- **iPhone and iPad:** iOS only notifies an app on the Home Screen. In Safari, use **Share → Add to
  Home Screen**, then open Clawdline from the Home Screen and subscribe there.

Labels below use Traditional Chinese examples with their English meaning; the console supports other interface languages.

## Turn them on

1. In the console, open **設定** (Settings) and find **通知** (notifications).
2. Press **通知我** (notify me) — "when a session is waiting for an answer" — and allow
   notifications when the browser asks.
3. Press **送一則測試** (send a test).

**Check:** the page says **送出去了——馬上就會到這台裝置。** (sent) and the test notification
arrives on this device.

## What you are told about

| When | Condition and destination |
| --- | --- |
| A Session waits on a question or permission for **10 minutes** | Once per stop, at most 6 per hour per machine; it opens that Session. The `orchestrator_agent_notify` switch controls this. |
| An agent calls `clawdline notify --title … --body …` or a task's notification route | Subject to `orchestrator_agent_notify` and the agent notification limit; `--session` opens the named Session. |
| An agent creates an unsolicited human intervention Note | Subject to `orchestrator_agent_notify`; it opens the owning Session. |
| A blocking Board decision needs an answer | Once per pending decision, at most 30 per hour per machine; it opens the list. |
| A Board item enters `done` | One outbox notification for that completion; it opens the assigned Session while that Session is still present. |
| A schedule misses its run, dispatch is refused, or its file cannot be read | Missed and refused runs require **Notify on failure** on the schedule. An invalid file is reported when its fingerprint changes. These open the list. |
| A capacity row crosses a notification threshold | The capacity monitor sends the recorded notice; it opens the list. |
| A task completion notice becomes a dead letter | Once for that dead letter; it opens the root Session while present. |
| A Cloud or running macOS app release passes its production checks | With **Notify me when Cloud or app releases succeed** on, one notification per build and release type; each opens the list. A failed check sends none. |
| You press **Send a test** | Only the asking device receives it. It opens the selected Session when the machine can still verify it; otherwise it opens the list. |

Tapping a waiting notification opens that session. A newer notification about the same session
replaces the older one.

The Go Settings page also displays switches for Session delivery and fan-out.
Those settings are stored, but this daemon currently has no sender that reads
them. Release notifications are sent by the shipped deployment checks after
success, and the deployment switch is off by default.

The `orchestrator_agent_notify` switch is on by default. When it is off,
`clawdline notify` is refused with `agent_notify_disabled`.

## Turn them off

Press **停掉** (stop) under **通知** on the same device. To stop a browser from asking at all,
change the site's notification permission in the browser's own settings.

## Troubleshooting

| You see | Do this |
| --- | --- |
| **這個瀏覽器沒辦法顯示通知。** (this browser cannot show notifications) | The page is not secure, or this is iOS outside the Home Screen. See "Before you start" |
| **這個網站的通知被擋掉了。** (notifications are blocked for this site) | Allow notifications for the site in the browser's settings |
| The test says the machine has no subscription for this device | Turn notifications off and on again on that device |
| Nothing arrives, but the test works | The session was answered within ten minutes, or the hourly limit was reached |

## Deeper

- [push.md](../push.md) — the Web Push implementation, what never leaves the machine, retries and
  the ten-minute measurement (in Chinese).
