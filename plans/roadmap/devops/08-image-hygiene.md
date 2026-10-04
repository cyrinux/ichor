# D8. Image hygiene

Status: **partial**. Size S.

## What exists today

- `inventory.go` `imageFlags`: per app `Unpinned` (`:latest` or untagged without digest) and
  `Drift` (one repository running several tags). Android shows them (`AppsGrid.kt`,
  `model/Inventory.kt` `attention`); **iOS ignores them** although it gets the same JSON.
- `images.go` `NodeImages`: the CRI image list of a node (Images screen), no analysis.
- `kube_pods.go` `podStatus` shows `ImagePullBackOff`/`ErrImagePull` but drops the waiting
  message. Only netperf explains its own pull failures (`netPerfPullFailures`).

## Plan

1. **iOS parity**: show the unpinned / drift badges and the attention state in `AppsView`.
2. **Images report** (Apps screen → "Images" filter, os:reader, from the inventory already loaded):
   unpinned images, repos with several tags (which nodes run which), images by registry
   (docker.io rate limits), and per node the CRI images no running container uses (disk space
   hint, read-only: Talos has no image GC call). Pure Go on `inventory` data: `ImageReport(json)`.
3. **Pull failures explained** (os:admin, Kubernetes): keep `state.waiting.message` in
   `mapPod`; `explainPull(message)` maps the common cases to plain words: not found
   (tag/repo typo), unauthorized (missing/expired `imagePullSecrets`, names the secret),
   `toomanyrequests` (Docker Hub limit, suggest a mirror: links to D3 registry-mirror snippet),
   TLS/x509, DNS/timeout (node cannot reach the registry: offer the S5 network tools).
   Shown on the pod row and in the AI diagnosis context.

Tests: table tests on messages from containerd. Size: 1 = S, 2 = S, 3 = S.
