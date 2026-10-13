# Notifications

This page lists the notifications the current Go daemon can send. A browser can
also send a test notification to its own device.

## How it works

Notifications are standard Web Push. The daemon on your machine sends them itself, over outbound
HTTPS to your browser's push service; no Clawdline account is needed. Each browser or phone that
should be told subscribes once.

The switches under **When to notify me** belong to the selected machine. You
can change them from the hosted console even when the browser you are using
cannot receive notifications. That browser's permission and subscription are
controlled separately under **This device**. A paired device needs Send
permission to change the machine's switches.

## Before you start

- A browser can subscribe only on a secure page: `http://127.0.0.1:7727` on the machine itself,
  your own tunnel's HTTPS address, or the hosted console at app.clawdline.com
  ([remote-access.md](remote-access.md)). An address on your local network (`http://192.168…`)
  cannot.
- **iPhone and iPad:** iOS only notifies an app on the Home Screen. In Safari, use **Share → Add to
  Home Screen**, then open Clawdline from the Home Screen and subscribe there.

Labels below use Traditional Chinese examples with their English meaning; the console supports other interface languages.

## Turn them on

1. In the sidebar, open **通知** (Notifications).
2. Press **開啟這台裝置的通知** (turn on on this device) and allow
   notifications when the browser asks.
3. Press **送一則測試** (send a test).

**Check:** the page says **送出去了——馬上就會到這台裝置。** (sent) and the test notification
arrives on this device.

## What you are told about

| When | Condition and destination |
| --- | --- |
| A Session waits on a question or permission for **10 minutes** | `push_on_waiting`: once per stop, at most 6 per hour per machine; it opens that Session. When unset, this key inherits the older `orchestrator_agent_notify` choice. |
| An agent calls `clawdline notify --title … --body …` or a task's notification route | Subject to `orchestrator_agent_notify` and the agent notification limit; `--session` opens the named Session. |
| An agent creates an unsolicited human intervention Note | Subject to `orchestrator_agent_notify`; it opens the owning Session. |
| A blocking Board decision needs an answer | `push_on_board_decision`: once per pending decision, at most 30 per hour per machine; it opens the list. |
| A Board item enters `done` | `push_on_board_done`: one outbox notification for that completion; it opens the assigned Session while that Session is still present. |
| A schedule misses its run, dispatch is refused, or its file cannot be read | `push_on_schedule_failure`: missed and refused runs also require **Notify on failure** on the schedule. An invalid file is reported when its fingerprint changes. These open the list. |
| A capacity row crosses a notification threshold | `push_on_capacity`: the capacity monitor sends the recorded notice; it opens the list. |
| A task completion notice becomes a dead letter | `push_on_dead_letter`: once for that dead letter; it opens the root Session while present. |
| A Cloud or running macOS app release passes its production checks | `push_on_cloud_release_success` and `push_on_app_release_success` are separate. Unset keys inherit the older `push_on_deploy` choice; a fresh installation has both off. Each opens the list. |
| An App install or Cloud production check fails | `push_on_release_failure` sends one notification for that failed release command and build. An unset key inherits `push_on_deploy`; a fresh installation has it off. It opens the list. |
| You press **Send a test** | Only the asking device receives it. It opens the selected Session when the machine can still verify it; otherwise it opens the list. |

Tapping a waiting notification opens that session. A newer notification about the same session
replaces the older one.

The Notification page lists only events with a working sender. Legacy Session
delivery and fan-out settings remain stored, but this daemon currently has no
sender for either. Smart wording has no active completion sender to change.
The page explains these gaps without offering switches that would have no effect.

The `orchestrator_agent_notify` switch is on by default. When it is off,
`clawdline notify` is refused with `agent_notify_disabled`.

## Describe a notification situation

Under **用一句話設定通知**, describe the situation in a sentence, then press **理解這句話**. The page shows the precise condition it understood before **儲存這條規則** becomes available. For example: "當 Session 等我回答時通知我"; "當 Session 等我回答超過 5 分鐘時通知我"; or "當名稱包含「部署」的 Session 等我回答時通知我". An unrecognized or unsupported situation is refused and not saved. The rule parser accepts the displayed examples and their answer/permission variants in English and Taiwan Traditional Chinese. It does not ask a paid external language model to infer new triggers.

A sentence without a wait time sends when the machine first observes the Session waiting. A sentence with a wait may specify 1–120 minutes. A matching rule replaces the standard ten-minute timing for that wait and works independently of the standard waiting switch. A name condition is case insensitive. The notification opens the waiting Session. Each stop produces at most one push, even if several rules match or the setting changes while it waits. The existing six-per-hour machine limit still applies.

Rules are stored on the chosen machine, up to 16 per machine. They can be disabled or deleted on the Notifications page. A disabled rule no longer matches; the standard waiting setting applies if no other enabled rule matches. Existing waiting notifications cannot be retracted or repeated by editing rules.

These rules currently cover **Session waiting for an answer or permission**. Other notification events retain their individual switches above.

## Turn them off

Press **關閉這台裝置的通知** under **通知** on the same device. To stop a browser from asking at all,
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
