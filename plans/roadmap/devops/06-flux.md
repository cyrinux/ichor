# D6. Flux

Status: **missing** (`plans/argocd/README.md` phase 5 and open question 3). Size L.
Flux is only recognised as an app in the inventory (`appcatalog.json`, `demo_inventory.go`).

## Goal

The same "pilot GitOps from the phone" Argo CD got, for Flux: see Kustomizations and
HelmReleases with their source, readiness and last applied revision; reconcile, suspend, resume.

## Key decisions

| # | Decision | Why |
|---|----------|-----|
| F1 | **CRDs only**, like Argo CD: `kustomize.toolkit.fluxcd.io/v1` Kustomization, `helm.toolkit.fluxcd.io/v2` HelmRelease, sources `source.toolkit.fluxcd.io/v1` GitRepository, OCIRepository, HelmRepository, HelmChart, Bucket. Detection via `readAPIGroups`. | No Flux CLI or token needed. |
| F2 | **Actions** = merge patches the `flux` CLI itself uses: reconcile = annotation `reconcile.fluxcd.io/requestedAt=<RFC3339 now>` (on the object, and "with source" on its source first); suspend/resume = `spec.suspend`; HelmRelease force/reset = `reconcile.fluxcd.io/forceAt` / `resetAt` (same value as requestedAt). | Matches `flux reconcile/suspend/resume`. |
| F3 | **One "GitOps" screen**: an Argo CD tab and a Flux tab (only the detected ones). Overview GitOps card counts both. | Answers the open question in plans/argocd; most clusters run one of them. |
| F4 | Health = `Ready` condition (+ `Reconciling`, `Stalled`), `lastAppliedRevision` vs source `artifact.revision`, `inventory.entries` for the resource list, dependency chain from `spec.dependsOn` drawn as a small tree. | All in status. |
| F5 | Join with node health (Argo D8 pattern) through the inventory's pods. | Ichor's advantage. |

## Go

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
