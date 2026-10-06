# D6. Flux

Status: **implemented**: Go core (`kube_flux*.go`, `just probe flux`, `just probe flux-action
KIND NAMESPACE NAME ACTION`), Android and iOS screens. Remaining: on-device checks against a real
Flux, alerts (phase 4), the dependsOn tree and events. Diff (phase 5): Kustomization diff done in Go
(`kube_diff*.go` shared with the future Argo CD diff, `kube_flux_{artifact,build,diff}.go`, `just probe
flux-diff KIND NAMESPACE NAME`), Android (`ui/flux/FluxDiffScreen.kt`, `ui/diff`) and iOS
(`FluxDiffView.swift`, `DiffViews.swift`) screens; the HelmRelease steps remain.

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
| F6 | **Kustomization**: read the source's artifact (the tarball source-controller already built) by **exec `cat <storage-path>/<status.artifact.path>`** in the source-controller pod (`manager` container, `--storage-path` arg, `/data` by default; the image is Alpine), the service proxy (`…/services/source-controller:80/proxy/<artifact.path>`) only when exec is refused; untar in memory (size cap); `kustomize build` of `spec.path` with `sigs.k8s.io/kustomize/api/krusty` on an in-memory filesystem, after editing the path's kustomization.yaml in place (generated when there is none) with `targetNamespace`, `namePrefix`/`nameSuffix`, `patches`, `images` (merged field by field) and `components`: a port of `fluxcd/pkg/kustomize` `GenerateManifest`; apply `spec.postBuild` (`substitute`, `substituteFrom` ConfigMaps/Secrets, newlines removed, not strict, `substituteStrategy`) with `github.com/fluxcd/pkg/envsubst`; then `commonMetadata` and the `kustomize.toolkit.fluxcd.io/name` and `/namespace` labels on each object's own metadata, as the controller does after the build; then a **server-side apply dry run** per object (`PATCH …?dryRun=All&force=true&fieldManager=kustomize-controller`, `application/apply-patch+yaml`) and a diff of the result against the live object. | Same steps as `flux diff ks`. The dry run applies defaults, webhooks and field ownership, so only what a reconcile would really change shows (a field someone added by hand and Flux does not own stays, as it would). Exec first: see the spike. |
| F7 | **Prune**: objects in `status.inventory.entries` no longer rendered are listed as "would be deleted" when `spec.prune` is true. | `flux diff` reports them too. |
| F8 | **SOPS**: objects that are encrypted (`sops:` key) are not decrypted (the key is in the cluster, never on the phone): listed as "encrypted, not compared". | No secrets leave the cluster. |
| F9 | **HelmRelease, step 1 (S)**: when `spec.driftDetection.mode` is `enabled` or `warn`, show the drift helm-controller already found (status condition, `DriftDetected` events with the changed fields). Step 2 (L, after a spike): render the chart from the HelmChart artifact with `values` + `valuesFrom` via the Helm SDK, dry run and diff like F6. | Step 1 costs nothing to the binary; step 2 pulls in the Helm SDK. |
| F10 | **Normalisation and redaction** shared with Argo: drop `managedFields`, `resourceVersion`, `generation`, `uid`, `creationTimestamp`, `status`; Secrets' `data`/`stringData` values replaced by an HMAC under a key of that diff only, so a change still shows but a short value cannot be looked up; values read from Secrets for substitution masked wherever they land; unified diff per resource, ≤ 64 KB each, timeout 60 s overall. | One diff code path for both GitOps tools. |

Go: `KubeFluxDiff(cfg, ctx, server, kind, ns, name string) (json)` →
`{resources: [{group, kind, namespace, name, change: created|changed|deleted|encrypted|unchanged, diff}], warnings}`,
read only (the dry run writes nothing), `just probe flux-diff KIND NAMESPACE NAME`. Fixtures:
a small artifact tarball with a kustomization.yaml, postBuild variables, a pruned object and a
SOPS Secret; demo diff in `kube_flux_demo.go`.

UI: **Show diff** on the Flux app detail (Kustomization; HelmRelease when drift detection is on),
also offered in the reconcile confirmation. The diff screen is Argo's: one collapsible block per
resource, badge per change kind, monospace red/green lines; "no changes" when everything matches.

### Spike results (2026-10-06)

Throwaway kind cluster (control plane + worker), Flux 2.9 `flux install` defaults, podinfo
GitRepository and Kustomization; drift made by hand on a suspended Kustomization.

- **Binary size**, linux/arm64, stripped, Ichor's Go core with and without: krusty adds
  **+3.8 MB raw / +1.36 MB gzipped** per ABI; Flux's `envsubst` adds nothing measurable.
  `github.com/fluxcd/pkg/kustomize` (the controller's own generator) adds +24 MB raw: it pulls
  controller-runtime and client-go. Rejected; its generator logic (a few hundred lines) is
  reimplemented on krusty instead.
- **Service proxy is blocked across nodes**: Flux installs `allow-egress` in `flux-system`,
  which only lets in pods of that namespace. With source-controller on a worker, the API server
  (host network on the control plane) gets `dial tcp …:9090: i/o timeout`. It only worked on a
  single node. Most real clusters will hit this.
- **Exec works whatever the NetworkPolicy**: `cat /data/gitrepository/<ns>/<name>/<rev>.tar.gz`
  through Ichor's WebSocket exec (`kube_exec.go`, binary-safe): 402 KB in 44 ms. Needs a raised
  output cap for this call (64 MiB), the default is 1 MiB. Port-forward also works but needs a
  protocol Ichor does not speak yet.
- **End to end** through Ichor's Go client: krusty build 3 ms; the dry-run diff showed exactly
  the drift a reconcile would revert (`minReadySeconds` changed, a deleted HPA as created) and
  left a hand-added annotation alone, as server-side apply does. Missing the controller's
  labels made every object show a spurious label removal: hence adding them (F6).
- A diff library is needed (`github.com/pmezard/go-difflib`, tiny) or a small LCS of our own.

Not covered: SOPS (F8 stays "not compared"), remote bases, OCIRepository and Bucket sources
(same artifact storage, so the same exec path), the HelmRelease steps.

Open questions:

1. Remote bases (`resources: - https://…`) in a kustomization: fetch them (network from the
   phone) or report them as not compared? Default: not compared, like SOPS.
2. A source in another namespace: Ichor cannot see whether the controller runs with
   `--no-cross-namespace-refs`. Default: diff anyway and say the controller may refuse it.

## Phases

1. Go. (M) 2. Android. (M) 3. iOS. (M) 4. Alerts. (S) 5. Diff: spike (done), Kustomization diff
(M), HelmRelease drift (S), HelmRelease render (L).
