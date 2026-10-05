# Roadmap: DevOps, sysadmin and UX features

Status: **planning**. Nothing here is built yet. DevOps comes first; each DevOps feature has its
own plan in [devops/](devops/). Sysadmin and UX are planned in [sysadmin.md](sysadmin.md) and
[ux.md](ux.md); a feature gets its own folder (like `plans/data-services/`) when work on it starts.

Every idea was checked against the code (Go core, Android, iOS) on 2026-10-04. Status:
**exists** = already shipped, nothing to plan (sometimes small gaps are listed);
**partial** = something is there and the plan improves it; **missing** = new.

## DevOps: change the cluster safely from the phone

| # | Feature | Status today | Plan | Size |
|---|---------|--------------|------|------|
| D1 | Node maintenance (cordon → drain → reboot → wait Ready → uncordon) | **implemented** (M8 "drain first" for upgrades remains) | [01-node-maintenance.md](devops/01-node-maintenance.md) | L |
| D2 | Small kubectl actions (scale, suspend CronJob, Deployment rollback, previous logs, pod logs from Workloads) | **implemented** (Argo self-heal warning remains) | [02-kubectl-actions.md](devops/02-kubectl-actions.md) | M |
| D3 | Machineconfig patches in try mode | **partial**: machine config is read-only (`machineconfig.go`); no `ApplyConfiguration` anywhere | [03-machineconfig-try-patches.md](devops/03-machineconfig-try-patches.md) | L |
| D4 | Rolling Talos upgrade (whole cluster), extension check, later `upgrade-k8s` | **partial**: one node at a time with a pre-flight plan, lock, progress (`upgrade*.go`, `ui/upgrade`) | [04-rolling-upgrade.md](devops/04-rolling-upgrade.md) | L |
| D5 | Argo CD diff and commit links | **missing** (deliberately left out of v1, `plans/argocd` D7) | [05-argocd-diff.md](devops/05-argocd-diff.md) | M |
| D6 | Flux | **implemented** (Go, Android, iOS); alerts remain | [06-flux.md](devops/06-flux.md) | L |
| D7 | cert-manager renew, Ingress/Gateway TLS expiry | **partial**: certificates and alerts exist read-only (`kube_certmanager.go`) | [07-certificates.md](devops/07-certificates.md) | S |
| D8 | Image hygiene (unpinned list, ImagePullBackOff explained) | **partial**: per-app `unpinned`/`drift` flags on Android only | [08-image-hygiene.md](devops/08-image-hygiene.md) | S |
| D9 | Argo CD freeze (deny sync window per app / namespace / project, windows screen) | **partial**: Go core done; Android and iOS to do | [09-argocd-freeze.md](devops/09-argocd-freeze.md) | M |

## Sysadmin: keep the platform alive

| # | Feature | Status today | Size |
|---|---------|--------------|------|
| S1 | etcd care | **exists**: snapshot (in clear), rolling defrag, disarm, forfeit, planned member remove. Plan: [encrypted snapshots](../etcd-encrypted-snapshot/README.md) first, then node reset + guided control-plane replacement, NOSPACE playbook | M |
| S2 | Alertmanager | **partial**: Prometheus queries/presets exist, Alertmanager is excluded from discovery | M |
| S3 | Push without polling (relay, UnifiedPush/ntfy) | **missing**: WorkManager / BGAppRefresh polling, active cluster only | L |
| S4 | Packet capture | **exists** (`pcap*.go`, `ui/capture`). No plan beyond small gaps | — |
| S5 | Node network tools (DNS, ping, port, traceroute) | **partial**: netstat + interactive debug shell with snippets | M |
| S6 | Storage: fill alerts, trends, disk wipe | **partial**: mounts, volumes, SMART, du exist; no alerts | M |
| S7 | Hardware sensors (temperatures, fans, throttling) | **missing** | S |
| S8 | Join a node from the phone | **missing** | XL |
| S9 | Action audit log | **missing** | M |
| S10 | Omni | **missing** | XL |

## UX: calm, fast, glanceable

| # | Feature | Status today | Size |
|---|---------|--------------|------|
| U1 | Actionable notifications | **partial**: tap only; one category on iOS with no actions | M |
| U2 | Live progress outside the app | **partial**: Android upgrade foreground service only | M |
| U3 | Global search | **missing** (per-screen filters only) | M |
| U4 | History and uptime | **partial**: only the last snapshot is kept | M |
| U5 | Favorites and runbooks | **partial**: Overview card layout and top-bar arrangement (Android, iOS) | M |
| U6 | Wear OS / Apple Watch | **missing** | L |
| U7 | Shortcuts and assistants | **partial**: one launcher shortcut per cluster | M |
| U8 | Tablet / foldable layout | **missing** (iPad allowed but single column) | L |
| U9 | Shareable incident summary | **partial**: support bundle + diagnosis prompt share; recorder has no export | S |
| U10 | Onboarding (multi-QR import, demo tour) | **partial**: QR import (≈2.9 KB limit) and demo exist | S |
| U11 | Large clusters: dense home, namespace-first paged lists ([large-clusters.md](large-clusters.md)) | **partial**: lists are virtualised, but home draws every node and Kubernetes lists are one cluster-wide GET capped at 32 MiB | L |

## Order

0. **Encrypted etcd snapshots** (S1, small, closes a data-exposure risk).
1. **D1 node maintenance**, then D2 (they share the eviction, scale and patch helpers).
2. **D3 try patches**, then **D4 rolling upgrade**: D4 reuses D1's drain and the "gated steps"
   run screen built for D1.
3. D7 and D8 (small), then D5 and D6.
4. Sysadmin and UX: S9 audit log early (every DevOps action adds mutations), U2 live progress
   with D1/D4 (they are the long-running operations), U1 with S2.

## Conventions every plan follows

- **Go first**, one exported function per action, `kubeMutate`/`nodeAction` for mutations,
  `kubeReadJSON` with demo data for reads, a `just probe` subcommand, table tests with `httptest`.
- **Both apps**: a `TalosRepository.kt` call and a `TalosClient.swift` call per Go function;
  parity unless the plan says otherwise.
- **Gates**: the existing `Feature`/`TalosFeature` rules (`features.go`, `model/NodeFeatures.kt`);
  Kubernetes mutations need os:admin (`Feature.WORKLOADS`), Talos mutations os:operator or more.
- **Safety**: every destructive action shows what it will do first, demo contexts refuse
  mutations (`demoUnavailable`), long-running work uses the Listener + Run handle pattern
  (`StartUpgrade`) so it can be followed and stopped.
- **Privacy**: names go through `privacy.reveal`/`maskResult`; nothing new is sent off the phone.
- README and fastlane changelogs are updated when a feature ships, not before.
