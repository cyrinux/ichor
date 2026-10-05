# D6. Flux

Status: **implemented**: Go core (`kube_flux*.go`, `just probe flux`, `just probe flux-action
KIND NAMESPACE NAME ACTION`), Android and iOS screens. Remaining: on-device checks against a real
Flux, alerts (phase 4), the dependsOn tree and events, the diff (phase 5).

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

## Diff (phase 5)

What `flux diff kustomization` shows, from the phone: for a Kustomization or HelmRelease, how the
cluster would change if the controller reconciled now. Built with the Argo CD diff
([05-argocd-diff.md](05-argocd-diff.md)): same wire format, same diff screen.

Argo's trick (exec its CLI in a controller pod) does not carry over: Flux controller images do not
ship the `flux` CLI, and `flux diff` builds from a local checkout. So Ichor does what `flux diff`
does, in Go.

| # | Decision | Why |
|---|----------|-----|
| F6 | **Kustomization**: fetch the source's `status.artifact.url` (the tarball source-controller already built) through the API server service proxy (`…/services/source-controller:80/proxy/…`, as `kube_garage.go` does); untar in memory (size cap); `kustomize build` of `spec.path` with `sigs.k8s.io/kustomize/api/krusty` on an in-memory filesystem; apply `spec.postBuild` (`substitute`, `substituteFrom` ConfigMaps/Secrets) with Flux's own `envsubst` rules; set `spec.targetNamespace`, `commonMetadata`, `patches`, `images`, `components`; then a **server-side apply dry run** per object (`PATCH …?dryRun=All&fieldManager=kustomize-controller`, `application/apply-patch+yaml`) and a diff of the result against the live object. | Same steps as `flux diff ks`; the dry run applies defaults, webhooks and field ownership, so only real changes show. |
| F7 | **Prune**: objects in `status.inventory.entries` no longer rendered are listed as "would be deleted" when `spec.prune` is true. | `flux diff` reports them too. |
| F8 | **SOPS**: objects that are encrypted (`sops:` key) are not decrypted (the key is in the cluster, never on the phone): listed as "encrypted, not compared". | No secrets leave the cluster. |
| F9 | **HelmRelease, step 1 (S)**: when `spec.driftDetection.mode` is `enabled` or `warn`, show the drift helm-controller already found (status condition, `DriftDetected` events with the changed fields). Step 2 (L, after a spike): render the chart from the HelmChart artifact with `values` + `valuesFrom` via the Helm SDK, dry run and diff like F6. | Step 1 costs nothing to the binary; step 2 pulls in the Helm SDK. |
| F10 | **Normalisation and redaction** shared with Argo: drop `managedFields`, `resourceVersion`, `generation`, `uid`, `creationTimestamp`, `status`; Secrets' `data`/`stringData` values replaced by a hash so a change still shows; unified diff per resource, ≤ 64 KB each, timeout 60 s overall. | One diff code path for both GitOps tools. |

Go: `KubeFluxDiff(cfg, ctx, server, kind, ns, name string) (json)` →
`{resources: [{group, kind, namespace, name, change: created|changed|deleted|encrypted|unchanged, diff}], warnings}`,
read only (the dry run writes nothing), `just probe flux-diff KIND NAMESPACE NAME`. Fixtures:
a small artifact tarball with a kustomization.yaml, postBuild variables, a pruned object and a
SOPS Secret; demo diff in `kube_flux_demo.go`.

UI: **Show diff** on the Flux app detail (Kustomization; HelmRelease when drift detection is on),
also offered in the reconcile confirmation. The diff screen is Argo's: one collapsible block per
resource, badge per change kind, monospace red/green lines; "no changes" when everything matches.

Spike first: binary size of krusty (and later the Helm SDK) in the gomobile library, and whether
the source-controller service is reachable through the proxy on a default install (it serves
artifacts on port 80, `http` named port).

Open questions:

1. Remote bases (`resources: - https://…`) in a kustomization: fetch them (network from the
   phone) or report them as not compared? Default: not compared, like SOPS.
2. A source in another namespace: Ichor cannot see whether the controller runs with
   `--no-cross-namespace-refs`. Default: diff anyway and say the controller may refuse it.

## Phases

1. Go. (M) 2. Android. (M) 3. iOS. (M) 4. Alerts. (S) 5. Diff: spike (S), Kustomization diff
(M), HelmRelease drift (S), HelmRelease render (L).
