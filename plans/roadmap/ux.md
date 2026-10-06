# UX features

Status: **planning**. Checked against the code on 2026-10-04. Each section gets its own folder
when work starts. Conventions: [README.md](README.md#conventions-every-plan-follows).

Navigation today: Android one `NavHost` (`ui/Navigation.kt` `Routes`), Overview as hub (top-bar
icons `OverviewActions.kt`, overflow `ClusterMenu.kt`), node screens with tabs; iOS one
`NavigationStack` with `enum Route` (`IchorApp.swift`), external routing via `NotificationRouter`.
Android tab screens swipe between tabs (`ui/components/SwipeTabs.kt` `SwipeTabPager`; not
Import, whose QR tab would start the camera mid-swipe, nor Argo CD, whose rows swipe to sync); iOS segmented pickers still tap only.

## U1. Actionable notifications: partial → actions (M)

**Today:** Android `monitor/Notifications.kt` `postAlert` has one content intent (only certificate
alerts deep-link); iOS registers one category `"private"` with no actions; taps never open the node.

**Plan:**
1. Every alert opens its subject: node alerts → node screen, etcd → etcd, data/cert → their tab
   (`Routes.node`, `NotificationRouter` new cases).
2. Action buttons per `AlertKind`: node unreachable/not ready → **Open**, **Wake** (when a MAC
   is known, Android; iOS once WoL exists there), **Reboot** (opens the app to the confirm screen,
   never one-tap); etcd alarm → **Open etcd**; Alertmanager (S2) → **Silence 1h**; data issue → **Open**.
3. **Snooze** per alert key (1 h / until tomorrow), stored with the monitor state; both apps.
4. Destructive actions always go through the app (biometric `Authenticator` + existing confirm);
   only non-destructive ones (silence, snooze, wake) run from the notification after unlock.

## U2. Live progress outside the app: partial → all long operations (M)

**Today:** Android `UpgradeService` (foreground, ongoing notification, no progress bar); iOS
`UpgradeJob` background task + "keep open" notification; no Live Activities.

**Plan:**
- Android: one `OperationService` (generalised from `UpgradeService`, see D1) for upgrades,
  maintenance, cluster upgrade, try-patch countdown, rollouts, Argo/Flux syncs started from the
  phone, netperf; determinate progress (`setProgress`) where known, a Stop action.
- iOS: a Live Activity (ActivityKit) in the `TalosWidget` extension: title, step, progress,
  countdown (`Text(timerInterval:)` for try mode); started by the same jobs; `NSSupportsLiveActivities`
  in `project.yml`. Updated locally while the app runs; the activity ends with the result.

## U3. Global search: missing (M)

A search field at the top of the Overview (Android `SearchBar`, iOS `.searchable` on the root):
nodes, pods, workloads, apps (inventory), Argo/Flux apps, services, namespaces of the current
cluster (and "in other clusters" from their last-known data: `OfflineCache.kt` / `LastKnownStore.swift`).
Index built in memory from data already loaded plus one `KubePods`/`KubeWorkloads` call when
os:admin. Results grouped by type, each opens its existing route. Recent searches and recently
opened items (per cluster, local). Fuzzy match on name and labels `app=`.

## U4. History and uptime: partial → history (M)

**Today:** `ClusterSnapshot` (`monitor/Snapshot.kt`, `IchorCore/Monitor.swift`) keeps only the
latest snapshot.

**Plan:** an encrypted ring per cluster (30 days, one compact sample per monitor run: per node
ready/reachable, etcd ok, alarms, mount usage for S6). Screens: per node and cluster availability
% over 24 h / 7 d / 30 d with a strip chart (gaps = phone did not poll, shown as such, never as
downtime); **"Since you last looked"** banner on the Overview: nodes that went down and came back,
reboots (boot time changed), version changes, new alerts, actions from the S9 log. Honest wording:
sampled every N minutes, from this phone only.

## U5. Favorites and runbooks: partial → pins and runbooks (M)

**Today:** Android `OverviewLayout` (card order/hidden, `OverviewEditor.kt`); nothing on iOS.

**Plan:**
1. iOS gets the Overview layout editor (parity).
2. **Pins**: star any node, pod, workload, app, Argo/Flux app → a "Pinned" Overview card with live
   status chips. Stored per cluster in the layout preferences.
3. **Runbooks** (later): a saved sequence of existing actions with waits and checks, for example
   "rollout restart `api` → wait rollout done → run CronJob `cache-warm` → check Argo app healthy".
   Steps limited to actions Ichor already has; each run shows the plan, needs one confirm, and is
   logged (S9). Built on the D1 run engine.

## U6. Wear OS and Apple Watch (L)

- Apple Watch: a WidgetKit complication first (reuse `TalosWidget` accessory families and the
  App Group snapshot; watchOS widget target), then a small watch app listing clusters and nodes
  with status, from the snapshot synced over WatchConnectivity (no Talos calls from the watch).
- Wear OS: a Tile + complication (`:wear` module) fed by the phone through the Data Layer API,
  plus notifications that already bridge. No talosconfig on the watch.

## U7. Shortcuts and assistants: partial → screens and intents (M)

**Today:** dynamic launcher shortcut per cluster (`shortcuts/ClusterShortcuts.kt`), iOS quick
actions (`QuickActions.swift`).

**Plan:**
- Shortcuts can open a screen: extend `EXTRA_OPEN`/`DeepLink` and the iOS router to `etcd`,
  `argocd`, `node/<name>`, `alerts`; pinned shortcuts from any screen's menu ("Add to home screen").
  Share links (`ichor://open?…`, `go/ichorgo/sharelink.go`) already route to etcd, Argo CD,
  Flux, a node, workloads and the Kubernetes tabs on both apps: reuse that routing.
- iOS **App Intents**: "Cluster status" (returns a sentence + snippet view: healthy / N nodes down),
  "Open node", "Run CronJob" (asks to confirm in the app). Siri and Shortcuts get them for free.
- Android: `shortcuts.xml` capabilities for App Actions where Google still supports them, and an
  exported, permission-protected intent for Tasker/automation (status query returns via result
  intent; mutating actions refused).

## U8. Tablet and foldable layout (L)

Android: `material3-adaptive` (`NavigationSuiteScaffold`, `ListDetailPaneScaffold`) for
Overview → node, Argo list → app, Workloads → rollout; landscape graphs full width.
iOS: `NavigationSplitView` on iPad (sidebar: clusters and sections, content: list, detail).
Do it screen family by screen family; phones unchanged.

## U9. Shareable incident summary: partial (S)

**Today:** support bundle zip (`support*.go`, scrubbed), diagnosis prompt share (anonymised by
`privacy.go`), incident recorder without export.

**Plan:** Go `IncidentSummary(cfg, ctx, anonymize bool)`: one Markdown page: what is wrong now
(the diagnosis report sections), the recorded timeline (key events only), drift differences,
recent alerts and the S9 actions taken. Anonymised by default with the existing mask. Share as
text or as a PNG card (rendered in-app). Button on the Insights recorder and the diagnosis screen.

## U10. Onboarding: partial (S)

**Today:** QR import (one QR, ≈ 2.9 KB, README), demo, lock onboarding, what's new.

**Plan:**
1. **Multi-QR import**: a tiny format `ichor:1/3:<base64 gzip chunk>`; a helper `just qr
   talosconfig` (and a one-liner documented for `talosctl config` users) prints the codes; the
   scanner collects parts in any order with a progress ring. Gzip + chunks makes any talosconfig fit.
2. **Demo tour**: 5 coach marks on the demo Overview (nodes, a degraded node, etcd, actions sheet,
   the cluster switcher), shown once, skippable, replayable from Settings.
