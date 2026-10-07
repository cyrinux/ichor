# D11. Helm rollback

Status: **implemented** (Go, Android, iOS). Size M. Read [../README.md](../README.md) for the
conventions.

## What exists around it

- **Helm releases** (kubeconfig work, `kube_helm.go`): list, and per release its summary,
  values, notes, manifest and history, decoded from the release Secrets without the Helm SDK.
- **Cluster checkup** (D10): failed and stuck releases; a release row opens the release.
- **Argo CD** rollback (`KubeArgoAction rollback`): syncs to an earlier history entry with
  auto-sync paused. Argo CD renders charts with `helm template`: no release Secrets, nothing
  for this feature to do.
- **Flux**: no rollback command. The helm-controller rolls back by itself when
  `spec.upgrade.remediation` asks for it; otherwise Git is the way back. A release rolled back
  by hand is upgraded straight back unless its HelmRelease is suspended.

## Decisions

| # | Decision | Why |
|---|----------|-----|
| H1 | **No Helm SDK.** The rollback is done here, the way Helm 3 does it: revision N+1 recorded `pending-rollback` (the lock), objects moved from the current manifest to the target's, N+1 `deployed` and the current one `superseded`. `helm history` agrees afterwards. | The SDK brings client-go, cli-runtime and kubectl into a phone binary. |
| H2 | **Three-way JSON merge patch** per object (original = current manifest, modified = target manifest, live): what the target sets is set, what the current manifest set and the target drops is removed, what neither names (defaults, other controllers, status) stays. Maps merge, lists are replaced (Helm's behaviour for custom resources; for built-in kinds the API server re-applies defaults and keeps allocated ClusterIPs and NodePorts). An object with an empty patch is not touched. | Faithful enough without strategic-merge metadata, and minimal: only what the bad upgrade changed moves. |
| H3 | **Plan first, dry run on the server.** `KubeHelmRollbackPlan` lists every create, update, delete and kept object, each dry-run (`dryRun=All`); a refusal shows on its object. The rollback re-plans and stops on any blocker. | D4 of the Argo CD plan: every action says what it will do. |
| H4 | **Refused, not guessed**: a chart with `pre-rollback`/`post-rollback` hooks (use `helm rollback`, which runs them), another operation in progress (`pending-*`), an object that exists without this release's ownership annotations, a kind the cluster no longer serves. | These are where a home-made rollback would go silently wrong. |
| H5 | **Flux-managed releases** (objects labelled `helm.toolkit.fluxcd.io/name`) are rolled back after suspending their HelmRelease; the plan says so. Resuming it returns to what Git says. | Otherwise the helm-controller undoes the rollback within one interval. |
| H6 | Install order is Helm's (`InstallOrder` by kind), deletions in reverse; objects marked `helm.sh/resource-policy: keep` stay. Revision 0 is the previous one, as for the CLI. History is not pruned (`--history-max`). | |
| H7 | On a failed step the new revision is recorded `failed` with the error, the current one `superseded`, as Helm does; the error names the object. | |

## Go (`go/ichorgo`)

- `kube_helm_rollback.go`: `KubeHelmRollbackPlan(cfg, ctx, server, ns, name, revision)` (JSON
  plan), `KubeHelmRollback(…)`; release Secret read/rewrite.
- `kube_helm_manifest.go`: manifest parsing (YAML nodes, duplicate keys tolerated), discovery
  per group version, install order, `threeWayMergePatch`.
- `just probe helm-rollback-plan NS NAME [REV]`, `just probe helm-rollback NS NAME [REV]`.

## UI (both apps)

- Release screen, history: older revisions get **Roll back**. A sheet loads the plan: now and
  after (revision, chart, app version), the Flux note, blockers, the changes with their action
  and dry-run error, then the confirmation.
- Checkup: a Helm release row opens the release screen.

## Later

- Run rollback hooks (create the hook Jobs, wait, apply delete policies) instead of refusing.
- `--history-max` pruning; `--force` (replace) and `--wait`.
