# D6. Flux

Status: **implemented**: Go core (`kube_flux*.go`, `just probe flux`, `just probe flux-action
KIND NAMESPACE NAME ACTION`), Android and iOS screens. Remaining: on-device checks against a real
Flux, alerts (phase 4), the dependsOn tree and events.

## Goal

The same "pilot GitOps from the phone" Argo CD got, for Flux: see Kustomizations and
HelmReleases with their source, readiness and last applied revision; reconcile, suspend, resume.

## Key decisions

| # | Decision | Why |
|---|----------|-----|
| F1 | **CRDs only**, like Argo CD: `kustomize.toolkit.fluxcd.io/v1` Kustomization, `helm.toolkit.fluxcd.io/v2` HelmRelease, sources `source.toolkit.fluxcd.io/v1` GitRepository, OCIRepository, HelmRepository, HelmChart, Bucket. Detection via `readAPIGroups`. | No Flux CLI or token needed. |
| F2 | **Actions** = merge patches the `flux` CLI itself uses: reconcile = annotation `reconcile.fluxcd.io/requestedAt=<RFC3339 now>` (on the object, and "with source" on its source first); suspend/resume = `spec.suspend`; HelmRelease force/reset = `reconcile.fluxcd.io/forceAt` / `resetAt` (same value as requestedAt). | Matches `flux reconcile/suspend/resume`. |
| F3 | **A Flux screen and Overview card of their own**, next to Argo CD's, each shown only when detected (the inventory's `flux` app, confirmed by the API groups). Changed from one tabbed "GitOps" screen: most clusters run one of them, and the Argo CD screens stay as they are. | Answers the open question in plans/argocd. |
| F4 | Health = `Ready` condition (+ `Reconciling`, `Stalled`), `lastAppliedRevision` vs source `artifact.revision`, `inventory.entries` for the resource list, dependency chain from `spec.dependsOn` drawn as a small tree. | All in status. |
| F5 | Join with node health (Argo D8 pattern) through the inventory's pods. | Ichor's advantage. |

## Go

The wire format is the Go types in `kube_flux.go` (`fluxStatus` and below); `kube_flux_demo.go`
shows every state. Kustomizations and HelmReleases are one list of "apps" (`kind` tells them
apart), the sources a second one; HelmCharts are not listed (a chart that fails shows on its
HelmRelease) but "reconcile with source" goes through the HelmRelease's HelmChart, as the CLI
does. Level: idle when suspended, critical when Stalled or Ready=False for a reason other than
waiting (DependencyNotReady, Progressing), warning while reconciling, ok when Ready. `pending`
is a requested reconcile the controller has not echoed back in `lastHandledReconcileAt` yet.
Suspending an object a parent Kustomization applies is allowed (its `owner` is shown): the parent
only reverts it when Git sets `spec.suspend` itself.

`kube_flux.go` (`KubeFlux` read + map, demo), `kube_flux_actions.go` (`KubeFluxAction(…, kind, ns,
name, action)` with `reconcile`, `reconcileWithSource`, `suspend`, `resume`, `force`, `reset`),
tests on fixtures, `just probe flux`.

## UI

Rename the Argo CD screen to GitOps with tabs (Android `ui/argocd`, iOS `ArgoCDView`); Flux list
(filter chips Ready/Not ready/Suspended/Stalled, group by namespace or source), detail (source,
revision with D5 commit link, conditions, inventory, dependsOn tree, events), actions sheet.
Optional alerts like Argo's (Ready=False for N minutes).

## Phases

1. Go. (M) 2. Android. (M) 3. iOS. (M) 4. Alerts. (S)
