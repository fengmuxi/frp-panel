# Email Notifications

frp-panel can send email alerts when key events happen to nodes, tunnels, and your account. Configuration is layered: **SMTP server (global) → your global switches (per account) → per-resource three-way setting (each node/tunnel individually)**.

## Where to Find It

| Entry | Location | Description |
| --- | --- | --- |
| Mail settings page | User avatar (bottom-left) → **Mail Alerts** | SMTP server config (admin only) + notification switches (all users) |
| Per-resource settings | Actions menu of a client/server, or the "···" menu of a tunnel → **Notification Settings** | Three-way: No Alerts / Global / Custom |

> The **Mail Alerts** entry in the bottom-left dropdown is visible to every logged-in user; the **SMTP Server Configuration** card is visible and editable by administrators only.

## 1. SMTP Server Configuration (Administrators Only)

Fill in the **SMTP Server Configuration** card at the top of the Mail Alerts page:

| Field | Description |
| --- | --- |
| SMTP Server | Mail server host, e.g. `smtp.example.com` |
| Port | Common ports: SSL/TLS `465`, STARTTLS `587`, plain `25` (auto-follows when switching encryption while the current port is a common one) |
| Encryption | **SSL/TLS** (implicit TLS, usually 465) / **STARTTLS** (usually 587) / **None** (usually 25) |
| Username / Password | SMTP credentials; leaving the password **empty keeps the saved one**. The password is stored encrypted and never returned by any API |
| From Address | Required From address, e.g. `noreply@example.com` |
| From Name | Optional display name; recipients see `Name <from@example.com>`, Unicode supported |

Steps:

1. Click **Send Test Email**: sends a generic test mail with the current form values (no need to save first) to the logged-in user; the success/error reason is shown as a toast.
2. Click **Save Changes** and confirm in the dialog.

::: tip
All notification emails are sent through this global SMTP configuration. If an administrator has not configured SMTP (or the test fails), no notifications will be delivered at all.
:::

## 2. Your Global Notification Switches (All Users)

The **Email Notification Settings** card below applies **only to your own account**. All switches are off by default:

**Alert switches (9 items)**

- Server offline / server online alerts
- Client offline / client online alerts
- Tunnel offline / tunnel online alerts
- Tunnel quota exceeded alert (auto-stopped after reaching the traffic quota)
- Tunnel schedule alert (auto started/stopped by its schedule)
- Login notification (sent to you after a successful login)

**Offline debounce**: configurable separately for server / client / tunnel (default 60 seconds, options from 30s to 10 minutes). The alert is sent only when the offline state **lasts longer** than the selected duration, so quick network blips won't bother you.

**Testing**: every switch row has a **Test** button that sends a sample email of that type to **you** using the saved SMTP configuration (verifies templates and delivery regardless of the switch state); the **Send Test Email** button at the bottom is a generic connectivity test.

Configuration takes effect after clicking **Save Changes**.

## 3. Per-Resource Three-Way Setting

Open the actions menu of any resource → **Notification Settings**. The top row has three side-by-side buttons:

| Option | Behavior |
| --- | --- |
| **No Alerts** (不提醒) | Highest priority: **all** alerts for this resource (offline, online, quota, schedule) are suppressed, regardless of global switches |
| **Global** (全局配置, default) | Follows the global switches on this page; the dialog shows a read-only list of your current global switch states to help you decide whether an override is needed |
| **Custom** (独立配置) | Uses this resource's own switch set, **taking priority over the global settings** |

- **Server / client** custom items: offline alert, online alert, offline debounce.
- **Tunnel** custom items: offline alert, online alert, quota exceeded alert, schedule alert, offline debounce.
- With **Custom** selected, alert types that are **not listed** are not sent (e.g. only online enabled means no offline alert).
- Choosing "Default" for the custom debounce means the built-in 60 seconds (not "no debounce").

Click **Save** to apply; the three-way selection and custom switches are restored when you reopen the dialog.

## 4. Priority & Delivery Rules

```
Resource = No Alerts    → nothing is ever sent for this resource (highest priority)
Resource = Custom       → the resource's own switches and debounce apply
Resource = Global / unset → the owner's global notification switches apply
                               (never saved before → legacy global defaults)
```

**Recipients**:

- Offline / online / quota / schedule alerts for servers, clients, and tunnels → the **owning user's** email (each user gets their own in multi-user setups; no cross-delivery)
- Login notification → **the user who logged in**
- Test emails → **the logged-in user**

::: warning
Recipients come from the account's Email field; accounts with an empty email receive no notifications (the skip reason is written to the log).
:::

## 5. When Each Alert Fires

| Alert | Trigger | Notes |
| --- | --- | --- |
| Server/client offline | Connection to Master lost and offline longer than the debounce | Not sent if it reconnects within the window |
| Server/client online | Node re-registered | At most once per 60 seconds |
| Tunnel offline | The tunnel disappears from frps' active push list for longer than the debounce | Not judged while frps is disconnected (push stops) — covered by the server offline alert |
| Tunnel online | A tunnel that previously triggered an offline alert resumes | Paired with the offline alert |
| Quota exceeded | Tunnel auto-stopped after reaching its quota | Includes quota period, direction, limit and usage |
| Schedule toggle | Tunnel auto started/stopped by its schedule | Only for tunnels with scheduling enabled |
| Login | Successful login | Includes username, source IP and time |

Manually stopping a tunnel or node is a deliberate action and does **not** trigger an offline alert.

## 6. Troubleshooting

1. **No emails at all**
   - Use **Send Test Email** first to verify SMTP; fix server/port/encryption/credentials based on the error.
   - Make sure the corresponding switch is enabled and saved (all are off by default).
   - Make sure the recipient account has a non-empty Email.
2. **A specific alert type never arrives**
   - Check whether the resource is set to **No Alerts**, or **Custom** without that item checked.
   - The debounce window has not elapsed yet (60s by default; tunnels need about 2 push rounds ≈ 1 minute).
   - The event was manual (manual stop/start does not trigger offline alerts).
3. **The global preview in the dialog looks stale**
   - The preview is read-only; change switches on the Email Notification Settings card and save.
4. **No emails after switching SMTP servers**
   - Save the changes and send a test email to confirm the new configuration works.

## See Also

- [FAQ](./faq.md)
- [All Configuration Options](./all-configs.md)
