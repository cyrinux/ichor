# D9. Argo CD freeze: hotfix live without being reverted

Status: **partial**: Phase 0 spike, Phase 1 (Go) and Phase 2 (Android) done; iOS to do. Size M. Read [../README.md](../README.md) for the conventions.

## Goal

On call, from the phone: "stop Argo CD from touching *this* for an hour" in two taps, fix by hand
(scale, edit, restart, the D2 actions), see what drifted, and end the freeze when the fix is in Git.
Then a single place lists every freeze so none is forgotten.

## What exists today

- Pause / resume auto-sync per Application (`KubeArgoAction` `autoSyncOff`/`autoSyncOn`): removes
  `spec.syncPolicy.automated`. **Refused on owned apps** (ApplicationSet or app-of-apps): the parent
  writes the spec back within seconds. Most real setups generate their apps, so the one existing
  "freeze" is unavailable exactly where it is needed.
- Projects are read (`/appprojects`) but only report a count of sync windows (`argoProject.SyncWindows`,
  shown in `ArgoSetsTab`); windows are not evaluated (plans/argocd "Wire format").
- D2 still has "warn when Argo CD self-heal will revert this scale/suspend" open.

## Key idea: a deny sync window on the AppProject

Argo CD's own freeze primitive. A `deny` window in `AppProject.spec.syncWindows` stops **automated
syncs, self-heal included**, for the apps it matches, while refresh keeps running: the app turns
OutOfSync and the drift *is* the hotfix, visible until it is committed.

| Why windows | |
|-------------|---|
| Work on owned apps | The window lives on the project, not the Application; ApplicationSets don't touch it. |
| Scope is native | `applications`, `namespaces` (destination), `clusters`, globs: app, namespace or whole project is one field. |
| Expire on their own | `schedule` + `duration`: Argo ends the freeze even if the phone is off or lost. |
| Visible to the team | Argo's UI and CLI show the window and "sync blocked" on each app. |
| Manual sync still possible | `manualSync: true` lets Ichor (and others) sync on purpose during the freeze. |

A one-shot window: cron of the start minute in UTC (`"42 14 5 10 *"`, `timeZone: "UTC"`) and
`duration: "1h"`. It never fires again until the same minute next year; expired ones are cleaned
up (below). Fields written: `kind`, `schedule`, `duration`, `timeZone`, `applications` |
`namespaces`, `manualSync`. Nothing version-specific (`description`, `andOperator` are newer and
not needed).

### Alternatives rejected

| Option | Why not |
|--------|---------|
| Pause auto-sync on the Application (exists) | Reverted on owned apps. Kept as the choice for unowned apps. |
| Patch the ApplicationSet template | Freezes every generated app; the ApplicationSet is itself in Git. |
| `argocd.argoproj.io/skip-reconcile` annotation | Stops refresh too, so the status goes stale and the drift is hidden; the ApplicationSet may drop the annotation. |
| Per-resource `sync-options` annotations | Don't stop self-heal of a field; one annotation per resource. |

## Marking Ichor's windows

The window object has no free-form field on every version, so the AppProject gets an annotation
`ichor.levis.name/freezes`: a JSON list of `{window, reason, createdAt, expiresAt}`, `window`
being the window's id (a hash of its whole JSON, keys sorted; windows have no name). An
unreadable annotation blocks writes rather than losing the records. A window matching an entry is Ichor's: it can be
extended or ended. Other windows come from Git (or another tool): they cannot be extended, and
removing one needs a warning (decision 1).

Every change is read-modify-write of `spec.syncWindows` + the annotation with the project's
`resourceVersion` (merge patch replaces the list whole); a concurrent change makes it fail with a
409 instead of being lost, and the app reloads before asking again. Other windows are written
back verbatim, fields Ichor does not know (`description`) included.

## The GitOps trap, again: projects in Git

Projects are usually Git-managed too (the reference cluster: an ApplicationSet with auto-sync,
prune, no self-heal). A window added live survives: Argo's three-way diff ignores a field absent
from both Git and the last applied config. It is lost when Git starts defining `syncWindows` for
that project (the list is replaced whole).

**Spike (2026-10-05, Argo CD v3.4.5, client-side apply)**: a deny window added live to a
Git-managed project (in neither Git nor `last-applied-configuration`) left the projects app
Synced after a hard refresh, and a manual sync of that app reported every AppProject
"unchanged" and kept the window. A live allow window on another project, there for months,
survived the same sync. Server-side apply was not tested (switching the field ownership of real
projects was out of scope for a spike): if the projects app uses `ServerSideApply=true`, the
sheet warns that the freeze may not survive a sync of that app.

The Go side detects the project's tracking annotation/label and the UI says "Project *x* is
managed by Argo CD app *y*" on the sheet.

## Go

Built: `kube_argocd_syncwindows.go` (read), `kube_argocd_freeze.go` (write), tests in
`kube_argocd_freeze_test.go`, demo in `kube_argocd_demo.go`.

- **Read** (`KubeArgoCD`): `argoProject.windows`: id, kind, schedule, duration, timeZone, scope
  (applications, namespaces, clusters), manualSync, `active`, `start`/`end` of the current or next
  occurrence (for an ended Ichor freeze that is next year's: use `ichor.expiresAt`), `error` when
  unreadable, `apps` matched, `ichor {reason, createdAt, expiresAt, expired}`. `syncWindows` (the
  count) stays for the current apps. `managedBy` / `managedServerSide`: the app applying the
  project from Git. Per app: `freeze {project, until, manualSync, byIchor, windows}` from the
  active deny windows matching it (Argo's rules: globs on name, destination namespace, cluster
  server or name; any selector, or all with `andOperator`). Cron evaluation reuses
  `cronschedule.go` (the CronJob one), in the window's time zone.
- **Write**: `KubeArgoFreeze(cfg, ctx, server, projectNamespace, project, action, optionsJSON)`,
  actions `freeze {applications | namespaces, minutes, manualSync, reason}` (`applications: ["*"]`
  for the whole project), `extend {window, minutes}`, `unfreeze {window, fromGit}`,
  `clearExpired`. 5 min–7 days, at most 20 names, reason ≤ 200 characters. Every change drops the
  project's expired Ichor windows; the same freeze asked twice within a minute is a no-op. Demo
  contexts refuse, as usual.
- **Safety nets**: the start minute and expiry come from the API server's clock (its `Date`
  header), not the phone's, so a phone off by minutes doesn't create a window already over. A
  freeze naming an app or a namespace no app of the project has is refused (Argo CD would accept
  a window freezing nothing: a typo, or a name the privacy mask did not map back; the listing
  teaches the masked namespaces). Extending a freeze into an identical one is refused.
- **A forgotten freeze comes back a year later** (the schedule repeats yearly): the read shows it
  `active` with `ichor.expired`; the apps run `clearExpired` when they see an expired Ichor freeze
  (Phase 2).
- `just probe argocd-freeze NAMESPACE PROJECT ACTION [OPTIONS_JSON]`.

## UX

### Freeze sheet (❄ "Freeze")

From the app hero (next to the auto-sync switch), a namespace/project group header in the apps
list, and the Argo CD section of an inventory app's sheet (where the fix actually happens).

```
❄ Freeze Argo CD
Scope     [ This app ] [ Namespace web ] [ Project apps ]
For       [ 15 min ] [ 1 h ] [ 4 h ] [ until 09:00 ] [ … ]
Reason    hotfix redis maxmemory                (optional)
[x] Allow manual syncs
Argo CD will not auto-sync or self-heal 3 apps (web-api, web-front,
web-worker) until 15:42. Changes made by hand stay until then.
                                              [ Freeze ]
```

Unowned app: a line "or pause auto-sync instead (until resumed)" keeps the existing action.

### While frozen

- App rows and hero: ❄ badge; hero banner "Frozen until 15:42 · 2 resources drifted ·
  Extend · Unfreeze".
- Apps screen: "Frozen" filter chip; Overview Argo card: one line "❄ 2 freezes · next ends 15:42".
- D2 hook (closes its open item): scaling or editing a workload whose app self-heals shows "Argo CD
  will revert this" with **Freeze for 1 h, then scale**.

### Sync windows screen (the menu)

From the apps screen menu and the project rows of the second tab (which already count windows).

```
Active
  ❄ apps · namespace web        ▰▰▰▰▱ 38 min left   Ichor · "hotfix redis"
     3 apps · manual sync allowed            [ +1 h ] [ End ]
  ⛔ infra · all apps            Git · Sat 01:00–05:00 weekly
Upcoming
  ⛔ infra · all apps            Git · in 2 d
Expired (Ichor)                                     [ Clear ]
  ❄ apps · web-api               ended 3 h ago
```

Swipe to end an Ichor freeze. Git windows can be removed too, behind a warning (decision 1).

### Ending a freeze

"End" (or the expiry, next time the app is opened) lists the drifted resources and asks:
"Argo CD will put these back as Git has them. Is the fix committed?" with **Sync now** / **Later**.
Android: a local notification 5 min before the end (scheduled at freeze time, no polling) with
**+1 h**; iOS: the same with a notification action.

## Android (built)

`model/ArgoFreezeViews.kt` (pure, tested in `ArgoFreezeTest`), `ui/argocd/ArgoFreezeUi.kt` (sheet,
card, dialogs), `ui/argocd/ArgoWindowsScreen.kt`, `monitor/FreezeReminders.kt`, the scale warning
in `ui/workloads/WorkloadSheet.kt`. Differences from the sketches above:

- The inventory app sheet shows "Frozen until …" on its Argo CD card; freezing itself is on the
  app page the card opens (the sheet lacks the project and the other apps a scope needs).
- The windows screen has "+1 h" / "End" / "Remove" buttons instead of swipes.
- No dialog at expiry: Ichor's ended freezes are cleared quietly on the next load.
- The reminder's tap opens the sync windows of the active cluster; its "+1 h" only acts when the
  freeze's cluster is still the active one (it says so otherwise). It is WorkManager work, not an
  exact alarm: under Doze it may come a few minutes late (Argo CD ends the freeze on time anyway).
- Ending a freeze from an app page says how many apps the freeze holds (a namespace or project
  freeze resumes them all). A namespace group header offers a freeze only when its apps share
  one project (a window belongs to one project).
- The D2 warning covers scaling (Deployments, StatefulSets), read from the Argo CD status already
  loaded; suspending a CronJob does not warn yet.

## Phases

| Phase | Content | Size |
|-------|---------|------|
| 0 | Spike: window on a Git-managed project survives a sync. **Done** (client-side apply) | S |
| 1 | Go read (window evaluation, per-app freeze) + write (`KubeArgoFreeze`), demo, tests, probe. **Done** | M |
| 2 | Android: freeze sheet, badges/banner/chip, sync windows screen, expiry notification, D2 hook. **Done** (see below) | M |
| 3 | iOS: the same | M |

## Decisions

1. Git-defined windows are **removable with a warning** in the menu: "This window comes from Git
   (project managed by app *y*): Argo CD may put it back on its next sync; remove it in Git too."
   Same typed-confirmation style as other destructive actions; the Go `unfreeze` action accepts
   them with `{window, fromGit: true}`.
2. Durations: default 1 h, 5 min–7 days.
3. Freezing a whole project is allowed, with the list of affected apps shown first.
