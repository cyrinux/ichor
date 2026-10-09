# D5. Argo CD diff and commit links

Status: **implemented** (CYR-64). Commit links: `kube_argocd_links.go`. Diff: `kube_argocd_diff.go`
with none of the options below: the application controller already computes the diff on every
reconciliation and caches it in Argo CD's Redis (`app|managed-resources|<app>|<version>`: per
resource the normalised live state and the predicted state after a sync, ignoreDifferences
applied, Secret values hidden). Ichor reads that key through a port-forward to the Redis pod
(`kube_redis.go`, a SCAN and a GET, the password from the `argocd-redis` Secret) and renders
each pair with the Flux diff's engine (`kube_diff.go`), so both tools share one redaction and
one screen. Option A (exec in an Argo CD pod) was built first and dropped: `argocd --core`
starts a headless API server inside the control-plane pod, heavy and hard to defend. Open:
Redis with TLS, and a per-resource diff in the sync sheet.

## What exists today

- `kube_argocd*.go`: apps read from the Application CRDs; actions through merge patches
  (`KubeArgoAction`: refresh, sync with prune/dryRun/selective resources, terminate, auto-sync, rollback).
- `kube_argocd_map.go` keeps sources (`repoURL`, `targetRevision`) and history revisions.
- The OutOfSync resource list says *what* drifted, not *how*. Links: only the app's `externalURLs`
  (`ArgoAppHero.kt` `Links`). iOS formats short commits (`IchorCore/ArgoCD.swift`) but links nowhere.
- `kube_exec.go` runs fixed commands in pods (used for Garage).

## Phase 1: commit links (S)

Build a browsable URL from `repoURL` + revision in Go (`argoCommitURL`): GitHub, GitLab,
Gitea/Forgejo, Bitbucket patterns from the host; SSH URLs normalised (`git@host:org/repo.git`);
unknown hosts → no link. Shown on the hero (current revision), each history row, and the
running sync. Helm charts: link the chart repo instead when it is an http(s) URL.

## Phase 2: the diff (M, spike first)

The desired manifests are not in the Application status; they come from the repo server.
Options, in preference order:

| Option | How | Trade-off |
|--------|-----|-----------|
| **A. `argocd app diff --core` in a controller pod** | `kube_exec.go` into the `argocd-application-controller` (or `argocd-server`) pod: the Argo image ships the `argocd` CLI; core mode uses the pod's service account and reaches the repo server in-cluster. Fixed argv: `argocd app diff <ns>/<name> --core`. | No credentials; works with SSO-only installs. Needs exec RBAC (cluster-admin has it) and depends on the CLI being in the image (true for upstream images; check `argocd version --client` first). |
| B. Repo-server gRPC through the API server proxy | Call `GenerateManifest` ourselves. | Reimplements Argo's normalisation; brittle. |
| C. argocd-server API with the initial admin secret | Log in with `argocd-initial-admin-secret`. | Often deleted; uses an admin password silently. Rejected. |

Go: `KubeArgoDiff(cfg, ctx, server, ns, name string) (json)` → per resource
`{group, kind, namespace, name, diff (unified, ≤ 64 KB, Secrets' data redacted)}`. Output parsed
from the CLI's `===== kind ns/name ======` sections. Timeout 60 s.

UI: on the app detail, **"Show diff"** for an OutOfSync app; the sync sheet shows the diff of
the selected resources before **Sync**. Monospace, collapsible per resource, red/green lines.

The normalisation, redaction and diff screen are shared with the Flux diff
([06-flux.md](06-flux.md) phase 5): build them once, for both.

## Open questions

1. Is exec into Argo CD pods acceptable to users? (It is what the Garage health already does.)
