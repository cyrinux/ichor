# Argo CD: see and pilot GitOps apps from the phone

Status: **implemented**: Go core (`kube_argocd*.go`, `just probe argocd`), Android and iOS
screens. Remaining: on-device checks against a real Argo CD, opt-in background alerts (phase 4),
diagnosis/support-bundle context.

Defaults chosen: a sync never prunes unless the user ticks it; Flux is deferred (phase 5).

## What exists today

- The inventory recognises Argo CD by its image (`quay.io/argoproj/argocd` → catalog id `argo-cd`,
  icon `argo-cd.webp`; `argo-rollouts` and `argo-workflows` too). That is all: the app shows the
  Argo CD tile like any other app.
- Everything needed to go further is already in place from the data-services work:
  `readAPIGroups` (`GET /apis`) for detection, the minimal REST client (`kubeClient.get` and
  `patch`), `withKube`, `kubeMutationError`, `validateKubeName`, `demoUnavailable`, the
  `Feature.WORKLOADS` gate (os:admin), the Overview card pattern and the opt-in background alerts.

## Key idea: drive Argo CD through its CRDs, not its API server

The os:admin kubeconfig Talos issues is cluster-admin, so the app can read and patch
`applications.argoproj.io` directly, the way `argocd --core` does. No Argo CD token, no
port-forward to `argocd-server`, no SSO, no extra credential to store.

| Action | How (one merge patch on the Application) | Notes |
|--------|------------------------------------------|-------|
| Refresh / hard refresh | `metadata.annotations["argocd.argoproj.io/refresh"] = "normal" \| "hard"` | The controller removes the annotation when done. |
| Sync | set `operation: {initiatedBy: {username: "ichor"}, sync: {revision, prune, dryRun, syncOptions, syncStrategy, resources}}` | Refused when `status.operationState.phase == Running` (would overwrite it). Multi-source apps use `revisions`. `resources` gives selective sync. |
| Terminate a running sync | `status.operationState.phase = "Terminating"` | The Application CRD has no status subresource, so a plain patch works (what the CLI does in core mode). |
| Pause / resume auto-sync | `spec.syncPolicy.automated.enabled = false \| true` (Argo CD ≥ 3.1, keeps prune/selfHeal); older: remove/restore `automated` | See "owned apps" below. |
| Rollback | sync to `status.history[i].revision` (and its source) | Only with auto-sync paused, otherwise the controller syncs straight back to HEAD. |

Everything the UI shows is already in the Application's `status`: `sync.status`/`revision`,
`health.status`/`message`, `operationState` (phase, message, start/finish, `syncResult.resources`
with per-resource result and hook phase), `resources[]` (kind, name, namespace, sync status,
health, `syncWave`, `requiresPruning`), `history[]` (revision, source, `deployedAt`),
`conditions[]` (`SyncError`, `ComparisonError`, `OrphanedResourceWarning`, …) and
`summary.images`. The one thing it lacks is the live diff and commit messages, which need the
repo server: left out (D7).

## Owned apps: the GitOps trap

Most real setups (the reference cluster included) generate Applications from an
**ApplicationSet** or an app-of-apps. Changing such an Application's `spec` (pausing auto-sync)
is reverted by its parent within seconds. So:

- Detect the owner: `ownerReferences` of kind `ApplicationSet`, or a parent Application found
  through the tracking label/annotation (`app.kubernetes.io/instance`,
  `argocd.argoproj.io/tracking-id`) pointing at another Application.
- For owned apps, **spec changes are disabled** with a one-line reason ("Managed by
  ApplicationSet *apps*: change it in Git"). Sync, refresh and terminate live in `operation` /
  annotations / status, which parents don't fight, so they stay available. Rollback is
  therefore only offered on unowned apps with auto-sync paused.

## Key decisions

| # | Decision | Why |
|---|----------|-----|
| D1 | **Detection like data services**: hint from the inventory (`argo-cd`), confirmed by the `argoproj.io` group in `/apis`. List Applications cluster-wide (`/apis/argoproj.io/v1alpha1/applications`), which also covers apps in any namespace. | No Kubernetes call for clusters without Argo CD. Mirrored registries are still caught by `/apis`. |
| D2 | **CRDs only** (see above). | Uses the credential Ichor already has; works with SSO-only Argo CD installs. |
| D3 | **Go maps Applications to a compact wire format.** Application objects are big (history, sync results); the phone gets a few KB per app, not tens. | Same split as data services: Go parses, Kotlin/Swift render. |
| D4 | **Actions are few and reversible**: refresh, sync (with options), terminate, pause/resume auto-sync, rollback. **No delete**, no spec editing, no parameter overrides. | Deleting an app (with cascade) is the one irreversible thing; a phone is the wrong place for it. |
| D5 | **Every action shows what it will do before doing it** (existing `Confirmation`): prune lists the resources that will be deleted (`requiresPruning`), rollback names the target revision and says auto-sync stays paused. | Prune and rollback are the dangerous ones. |
| D6 | **Gated by `Feature.WORKLOADS`** (os:admin), like the Kubernetes screen. | Same credential, no new role concept. |
| D7 | **No diff view, no commit messages** in v1. | They need `argocd-server`/repo server and its own auth. The resource list with OutOfSync markers says *what* drifted. |
| D8 | **Join with Talos node health** (the data-services D9 pattern): a Degraded app whose pods sit on a NotReady node says so at the top. | This is what Argo CD's own UI can't do and Ichor can. |

## UX

### Overview: a GitOps card (only when Argo CD is detected)

```
┌──────────────────────────────────────────────┐
│ [argo icon] GitOps              24 apps    › │
│ ███████████████████████▒▒░  health bar       │
│ ● 21 healthy  ● 2 progressing  ● 1 degraded  │
│ ⟳ syncing: cilium  wave 1/4  ▰▰▰▱            │
│ ⚠ grafana  Degraded · pod Pending on node-3  │
│            (node-3 NotReady)                 │
└──────────────────────────────────────────────┘
```

A segmented health bar, counts, the running syncs with their wave progress, and at most two
problem apps with their likely cause. Calm (one muted line) when everything is Synced + Healthy.

### Apps screen

- **Filter chips with counts** across the top: Degraded · OutOfSync · Progressing · Syncing ·
  Auto-sync off · Failed. "Sync all OutOfSync" appears when that chip is selected.
- **Group by** project / ApplicationSet / destination namespace.
- **Rows**: the app's **icon from the existing catalog** (matched on the Helm chart name or the
  images in `summary.images`: cilium, longhorn, traefik, cert-manager… already have icons), name,
  Argo-style health glyph (heart / spinner / broken heart) and sync glyph, chart@version or short
  revision, "deployed 3 h ago".
- **Swipe** right to sync, left to refresh; **long press** to multi-select and act on several.
- Pull to refresh; polls every 2 s while an operation is running, otherwise on screen open.

### App detail

- **Hero**: icon, name, big health + sync badges, project, destination, source (repo + path, or
  chart@version), auto-sync state (switch, disabled with the reason for owned apps), conditions
  as banners.
- **Operation card**: phase, message, elapsed time, "12 / 30 resources", the failing resources
  with their message, a Terminate button while running.
- **Sync waves timeline** (the centrepiece): resources grouped by `syncWave`, as vertical steps
  (−5 → 3). Each step shows kind icons with a status dot; during a sync the current wave pulses,
  done waves turn green, hooks show their phase. Setups that lean on waves (the reference cluster
  uses −5 … 3) finally see *where* a sync is stuck.
- **Resource rows** link into what Ichor already does: a Deployment/StatefulSet/DaemonSet opens
  its workload sheet (rollout restart), a Pod its logs and delete, anything with a node shows
  that node's Talos health. A tick per row gives selective sync.
- **History**: a timeline of deployments (revision, chart version, when). "Roll back to this" on
  unowned apps.
- **Sync sheet**: prune (with the list of what goes), dry run, force, apply out-of-sync only,
  server-side apply, replace. Defaults copied from the app's `syncOptions`.

### ApplicationSets and projects (second tab)

- ApplicationSets with their generated apps rolled up (worst health first), `ErrorOccurred` /
  `ResourcesUpToDate` conditions and progressive-sync step status.
- AppProject **sync windows**: "Syncs blocked by window until 06:00" on every affected app and in
  the sync sheet, before the user taps.

### Background (opt-in, off by default, like data-services alerts)

- Notify when an app turns Degraded, a sync fails, or an app stays OutOfSync with auto-sync on
  for more than N minutes.
- Android: an ongoing notification with wave progress while a sync the user started is running.
  iOS: a Live Activity for the same (later).

### AI diagnosis / support bundle

Degraded and failed apps (conditions, health messages, failed resources) go into the diagnosis
context and the support bundle, masked like the rest.

## Wire format

The JSON is defined by the Go types in `go/ichorgo/kube_argocd.go` (`argoStatus` and below);
`kube_argocd_demo.go` shows every state. Differences from the first draft: no sync-window
evaluation (projects only report how many windows they have), per-resource health is often
empty on Argo CD 3 (it no longer stores it in the Application), and the nodes of an unhealthy
app come from `unhealthyPods` (pods of its destination namespace that are not ready).

Auto-sync is paused by removing `spec.syncPolicy.automated` and keeping its prune/selfHeal in
the `ichor.levis.name/paused-automated` annotation, which resuming restores: unlike
`automated.enabled`, this works on every Argo CD version.

## Phases

| Phase | Content | Size |
|-------|---------|------|
| 1 | Go: `KubeArgoApps` (detect, list, map, owner detection, icon matching, wave progress), `KubeArgoAction` (refresh, sync, terminate, auto-sync, rollback) with guards, demo data with a sync in progress, fixtures, `just probe argo` | L |
| 2 | Android: Overview card, apps screen, app detail with waves timeline, sync sheet | L |
| 3 | iOS: the same | L |
| 4 | Opt-in alerts, sync progress notification; diagnosis and support-bundle context | M |
| 5 | (later) ApplicationSet tab polish, Argo Rollouts (pause/promote/abort a Rollout, same CRD approach), Flux (`kustomize.toolkit.fluxcd.io`, `helm.toolkit.fluxcd.io`: reconcile/suspend through annotations) | M |

## Open questions

1. Fixtures: OK to capture a few Applications, an ApplicationSet and an AppProject from the
   reference cluster (read-only `kubectl get … -o json`), anonymised before committing?
2. Sync defaults: mirror the app's own `syncOptions` and never prune unless ticked, or follow
   the app's `automated.prune`?
3. Should Flux get the same screen in v1 (a "GitOps" screen with an Argo tab and a Flux tab)?
   The demo cluster already shows Flux controllers.
