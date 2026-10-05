# U11. Large clusters: home at scale, namespace-first paged lists

Status: **implemented** (phases 1–5). Size L. Read [README.md](README.md) for the conventions.

## What exists today

Checked against the code on 2026-10-05.

**Home (Overview)**

- `NodesCard.kt` (Android) and `OverviewView.swift` (iOS) render **every** talosconfig node, uncapped:
  a chip per calm node in a `FlowRow`, a full row per node needing attention, a full row per node
  when expanded. The card is one `LazyColumn` item, so all chips/rows compose eagerly.
- Apps (`overviewTiles` + `MoreTile`) and Argo (`MAX_PROBLEMS = 2`) cards are already capped.
- Per-node fan-out: overview probe (`maxNodeFanout = 32`), live stats every 5 s
  (`CLUSTER_POLL_SECONDS`, every node), inventory (CRI containers of every node).

**Kubernetes lists**

- One cluster-wide GET per kind, full objects, no `limit`/`continue`, no Table format, no watch:
  `kube_pods.go` `/api/v1/pods`, `kube_workloads.go` `/apis/apps/v1/{kind}`,
  `kube_cronjobs.go`, routes, PDBs, CRDs.
- Bodies are read whole and capped at `kubeMaxBody = 32 MiB` (`kube_client.go`): around 3–5k
  pods the Pods tab **fails** outright (`TooLarge`).
- The namespace filter is client-side (`KubernetesScreen.kt` `namespace`, `model/Pods.kt`
  `podNamespaces`/`filteredPods`): chips come from the downloaded pods.
- A user whose RBAC only allows some namespaces cannot list cluster-wide, so every list fails.
- Rendering is already virtualised (`LazyColumn(items(…, key))`); the download is the problem.

## Goal

A 200-node, 20k-pod cluster stays glanceable on home and usable in the Kubernetes screens, on
mobile data, without breaking the small-homelab experience (which must not change visibly).

## Key decisions

| # | Decision | Why |
|---|----------|-----|
| L1 | **Home switches to a dense layout above a threshold** (`NODE_DENSE_THRESHOLD ≈ 24`): a health summary line ("187 ok · 3 degraded · 1 unreachable"), a grid of small status dots (tap = node, long press = actions), then problem nodes as full rows **capped at 5** + "N more". | Chips with hostnames stop being readable past a few dozen; problems must stay visible. |
| L2 | **"Expand" opens a Nodes screen** (search, site filter, health filter, `LazyColumn`) instead of growing the card inline once dense. Below the threshold, today's inline expand stays. | A card holding 200 rows defeats the home screen and composes eagerly. |
| L3 | **Multi-site: one line per site with its counts**, tap to drill into that site's dots. | Scales with sites as well as nodes. |
| L4 | **Live stats scale down**: above the threshold, poll every 15 s instead of 5 s. Keep the cluster total. Later: sample only problem nodes + those visible. | 200 nodes × every 5 s = 40 Talos calls/s from a phone. |
| L5 | **Namespace-first**: fetch `/api/v1/namespaces` (small), pick the scope, list with `/api/v1/namespaces/{ns}/…`. "All namespaces" stays an option. Remember the last scope per cluster; default to "All" only when the first page shows the cluster is small. | Most work happens in one namespace; also fixes namespace-scoped RBAC. |
| L6 | **If listing namespaces is forbidden**, offer a typed namespace (remembered per cluster) and use namespaces from the kubeconfig context. | Restricted users still get a working screen. |
| L7 | **Paged fetch, eager background loading, not load-on-scroll.** Kotlin/Swift call a page function (`limit=500`) in a loop and append rows as pages arrive; a thin progress bar shows "1,500 / ~4,000" (`remainingItemCount`). Sort, search and counts become exact once complete. | The API only returns namespace/name order; "unhealthy first", search, namespace chips and counts need the whole scope. A CrashLoop pod must not hide on page 10. Paging still bounds each response (no 32 MiB failure). |
| L8 | **Cap** the eager load (10k rows; 5k on metered networks). Past it: banner "Showing 10,000 of ~48,000 — pick a namespace", then **load-on-scroll in server order** with a note that sorting/search only cover what is loaded. | Rare fallback for "All namespaces" on huge clusters; memory and data stay bounded. |
| L9 | **Table format for lists** (`Accept: application/json;as=Table;v=v1;g=meta.k8s.io, application/json`, `includeObject=Metadata`): rows from server columns + owner refs from metadata. The full object is fetched when a row is opened. Falls back to JSON when the server refuses. | 10–20× smaller than full objects on mobile data. |
| L10 | **Image search and last termination** move to the detail view; list search covers name, status, node, owner. Images stay searchable when the scope was loaded in JSON (fallback / small clusters, < 500 pods). | Table rows do not carry container specs. |
| L11 | **Server-side selectors where they are exact**: node → its pods (`fieldSelector=spec.nodeName=`), workload → its pods (`labelSelector` from `spec.selector`), phase filters. Drill-downs may use load-on-scroll since their order does not matter. | Exact, cheap, no client post-processing needed. |
| L12 | **`410 Gone`** (expired continue token, ~5 min) restarts the load silently from page 1. **Refresh** keeps the old rows on screen and swaps when the new load completes. **Offline cache** stores only complete lists. | No empty flashes, no half lists served as truth. |
| L13 | **Hand-rolled `PagedLoad` state** (items, token, remaining, done, error) plugged into `LoadingViewModel`/`remember()`, not androidx Paging 3. | Paging 3 assumes on-scroll server-ordered loading, which L7 rejects. |

## Go

| File | Content |
|------|---------|
| `kube_client.go` | `getPage(ctx, path, query, limit, continueToken, table bool)` → items + `continue` + `remainingItemCount`; Table decoding (`metav1.Table` subset); `410` surfaced as a typed `Expired` error. |
| `kube_namespaces.go` | `KubeNamespaces(config, ctx, server)` → `{namespaces, forbidden}`. |
| `kube_pods.go` | `KubePodsPage(config, ctx, server, namespace, selector, continueToken, limit)` → `{pods, continue, remaining}`; `KubePods` kept for older callers, built on the page loop. |
| `kube_workloads.go`, `kube_cronjobs.go` | same page functions per kind (workloads: three kinds, one page each in parallel, merged by the caller). |
| tests | `httptest` server serving pages, a mid-load `410`, Table and JSON fallbacks, namespace-forbidden, `remainingItemCount` absent with selectors. |
| probe | `just probe pods-page <ns> <limit>`, `just probe namespaces`. |

## UI (both apps)

- **Home**: dense Nodes card (L1–L3), Nodes screen, live-stats cadence (L4).
- **Kubernetes screen**: namespace picker fed by `KubeNamespaces` (searchable sheet when > 15),
  scope remembered per cluster; progress bar while pages load; cap banner (L8); search hint when
  the list is incomplete.
- **Pod detail**: full object on open (images, last termination, containers).

## Phases

1. Home dense layout + Nodes screen + live-stats cadence, Android. (M)
2. Go: paging, namespaces, Table format, `410`, tests, probe. (M)
3. Android: namespace-first scope, `PagedLoad`, progress, cap fallback; Pods, then Workloads and CronJobs. (M)
4. iOS parity for 1 and 3. (M)
5. Drill-down selectors (node → pods, workload → pods) on the paged path; CRD lists (Argo, Longhorn…) on the same loop. (S)

Phases 1–5 are done.

## Phase 5 notes

- **Selectors** (`kube_pods_selected.go`): `KubeNodePodsPage` (`fieldSelector=spec.nodeName=`) and
  `KubeWorkloadPodsPage` (the workload's `spec.selector`, matchLabels and matchExpressions, as a
  `labelSelector`; an empty or unwritable selector lists nothing), both with an optional phase
  (`Running`, `!Succeeded`), page by page like `KubePodsPage`. `just probe node-pods` /
  `workload-pods`. No app screen lists the Kubernetes pods of a node or workload yet: the node
  Pods tab reads the containers through Talos CRI, and the rollout sheet already used the
  workload's selector (now with matchExpressions too).
- **App workloads** (app detail sheet, both apps): `KubeAppWorkloads` reads only the app's pods
  (one GET each when a namespace holds ≤ 8 of them, else that namespace's Table page by page)
  and each owner, instead of every pod and workload of the cluster; after a restart
  `KubeWorkloadsNamed` reads those found again (one deleted since drops out). With the privacy
  mask on, the masked namespaces and pod/workload names the app sends back map to the real ones
  (`learnNames`, learned from the inventory and the lists). The apps' `ownersOf` /
  `workloadOwners` moved into Go; Android's whole-cluster `pods()`/`workloads()` are gone.
- **Argo CD / Flux unhealthy pods**: only the namespaces of apps in trouble, one by one (one
  cluster-wide list past 16), with `fieldSelector=status.phase!=Succeeded`, paged.
- **Routes**: an app's pods are read one by one (≤ 8 per namespace) instead of listing their
  namespace; Ingress/HTTPRoute/Gateway lists stay cluster-wide (an HTTPRoute may point across
  namespaces) but are paged.
- **Network policies**: still cluster-wide (the view is the whole cluster's), paged, without
  finished pods (`status.phase!=Succeeded,status.phase!=Failed`).
- **CRD and other lists** go through `getList` (`kube_list.go`, the paged loop, `limit=500`,
  `410` restart): Argo CD, Flux, cert-manager, CNPG, Longhorn, MariaDB, Percona, Rook-Ceph,
  Velero, Dragonfly, Garage services, Prometheus discovery, network policies, routes, drain
  pods/PDBs, rollout pods/ReplicaSets. Their public functions and outputs are unchanged.
- **Left**: `KubePods`/`KubeWorkloads`/`KubeCronJobs` (whole cluster, already paged inside) stay
  for iOS' last-known domains; small namespace-scoped lists (HPAs, netperf's own namespaces)
  keep a single GET.

## Out of scope

- Watch / informers (pull-to-refresh is enough for now).
- Server-side text search (the API has none).
