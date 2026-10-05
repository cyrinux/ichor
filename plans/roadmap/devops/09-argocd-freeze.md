# D9. Argo CD freeze: hotfix live without being reverted

Status: **missing**. Size M. Read [../README.md](../README.md) for the conventions.

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
`ichor.levis.name/freezes`: a JSON list of `{window (kind, schedule, duration, applications,
namespaces), reason, createdAt, expiresAt}`. A window matching an entry is Ichor's: it can be
extended or ended. Other windows come from Git (or another tool): they cannot be extended, and
removing one needs a warning (decision 1).

Every change is read-modify-write of `spec.syncWindows` + the annotation with the project's
`resourceVersion` (merge patch replaces the list whole); a 409 is retried once.

## The GitOps trap, again: projects in Git

Projects are usually Git-managed too (the reference cluster: an ApplicationSet with auto-sync,
prune, no self-heal). Adding a window live should survive: Argo's three-way diff ignores a field
absent from both Git and the last applied config. It is lost when Git starts defining
`syncWindows` for that project (the list is replaced whole). **Spike first**: add a window to a
Git-managed project, push an unrelated commit, check it survives with client-side and
server-side apply. If it doesn't, the project's managing app gets frozen first by the same
mechanism (its own project), or the action explains why it can't freeze.

The Go side detects the project's tracking annotation/label and the UI says "Project *x* is
managed by Argo CD app *y*" on the sheet.

## Go

- **Read** (`KubeArgoCD`): `argoProject.Windows []argoSyncWindow` replacing the count:
  kind, schedule, duration, scope (applications, namespaces, clusters), manualSync, `active`,
  `start`/`end` of the current or next occurrence, `ichor *{reason, createdAt, expiresAt}`,
  `matches` (app count). Per app: `freeze *{project, until, scope, manualSync, byIchor}` from the
  active deny windows that match it (Argo's glob rules on name, destination namespace, cluster).
  Cron evaluation with `robfig/cron/v3` (what Argo uses).
- **Write**: `KubeArgoFreeze(cfg, ctx, server, projectNamespace, project, action, optionsJSON)`,
  actions `freeze {applications | namespaces, minutes, manualSync, reason}`, `extend {window,
  minutes}`, `unfreeze {window}`, `clearExpired`. Duration 5 min–7 days. `freeze` also drops this
  project's expired Ichor windows. Demo contexts refuse, as usual.
- `just probe argo-freeze PROJECT …`, table tests (glob matching, one-shot cron, expiry, 409 retry,
  Git windows untouched unless removed explicitly), demo data with one active freeze, one Git window, one expired.

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

## Phases

| Phase | Content | Size |
|-------|---------|------|
| 0 | Spike: window on a Git-managed project survives a sync (both apply modes) | S |
| 1 | Go read (window evaluation, per-app freeze) + write (`KubeArgoFreeze`), demo, tests, probe | M |
| 2 | Android: freeze sheet, badges/banner/chip, sync windows screen, expiry notification, D2 hook | M |
| 3 | iOS: the same | M |

## Decisions

1. Git-defined windows are **removable with a warning** in the menu: "This window comes from Git
   (project managed by app *y*): Argo CD may put it back on its next sync; remove it in Git too."
   Same typed-confirmation style as other destructive actions; the Go `unfreeze` action accepts
   them with `{window, fromGit: true}`.
2. Durations: default 1 h, 5 min–7 days.
3. Freezing a whole project is allowed, with the list of affected apps shown first.
