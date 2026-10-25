# Garage cluster layout from the app: design study

Status: study, nothing implemented yet. Written against Garage `main-v2` (admin API spec
v2.5.0) and the app as of October 2026.

## What the user should be able to do

1. **See the layout**: the current version, each node's role (zone, capacity or gateway, tags),
   how much of each node's capacity the layout actually uses, the layout parameters.
2. **List the layout versions**: the history (current, draining, historical), how far every
   node has acknowledged it, and whether a migration is stuck.
3. **Add a node to the layout** with everything Garage needs (zone, capacity or gateway role,
   tags), see what the new layout would move, then apply it. Also change or remove a role.

## What exists today

- **Transport.** Everything Garage-specific runs `garage json-api <Endpoint> [json]` inside a
  ready Garage pod through `pods/exec` (`go/ichorgo/kube_garage_api.go`). The CLI talks to the
  node over RPC with the node's own secret: no admin token, no admin port to reach. This is the
  only way the app has to Garage, and it already carries the maintenance actions (resync
  tranquility, block error report, repairs).
- **Two call shapes.** `garageRun` runs a fixed argv (`GetClusterHealth`, `GetClusterStatus`:
  cluster-wide, no payload). `garageCall[T]` is for *node-scoped* endpoints and wraps the body in
  `{"node": ..., "body": ...}`, the answer being `{success: {nodeId: T}, error: {nodeId: msg}}`.
  The layout endpoints are cluster-wide, so they need a third helper (below), not `garageCall`.
- **Data.** `garageInstance` already carries `layoutVersion` and, per node, `storage` (has a
  role), `zone`, `tags`, `dataAvail`/`dataTotal`, `up`, `draining`, `kubeNode`. The role's
  `capacity` from `GetClusterStatus` is read by Garage but dropped by the app.
- **UI.** Android `GarageTab` shows one `SectionCard` per instance: tiles, a "Details"
  expander (which prints nothing about the layout yet), the node rows with a ⋮ menu (tranquility)
  and a "Block errors" text button opening `GarageBlocksSheet`. iOS `GarageList` mirrors it with
  swipe actions and a context menu. Actions live in `GarageActions` (Android) and
  `TalosClient+DataServices.swift` (iOS), both thin wrappers over the Go facade, with
  `ResultToasts` for outcomes.
- **Gate.** Actions are offered when `inst.detailed && inst.pod.isNotEmpty()`: a full
  `cli-json` reading proves `garage json-api` works in that pod. Every action records an audit
  entry (`recordAction`) and is refused on the demo context.
- **garage-operator.** When the operator (`garage.rajsingh.info`) manages a cluster, the layout
  is declared by `GarageNode` CRs (`spec.zone`, `spec.capacity`, `spec.gateway`) and the operator
  stages and applies it itself (`GarageCluster.status.stagedLayoutVersion`, reason
  `layoutStaged`). The app already reads all of this (`kube_garage_operator.go`).

## Garage's layout model (verified in the source)

The admin API v2 and the `json-api` subcommand shipped with Garage 2.0; `json-api` takes the
endpoint name and an optional positional JSON payload (`-` reads stdin). Without a payload the
CLI sends `null`, then `{}`, so unit requests need none. An API error comes back as
`Error: <code> (<http>): <message>` on stderr, which `garageCommandError` already extracts.

Layout changes are **staged, previewed, then applied with the expected version number**:

| Endpoint | Request | Response |
|---|---|---|
| `GetClusterLayout` | none | `version`, `roles[]` (`id`, `zone`, `tags[]`, `capacity` bytes or `null` = gateway, `storedPartitions`, `usableCapacity`), `parameters.zoneRedundancy`, `partitionSize`, `stagedRoleChanges[]` (`id` + either `{remove: true}` or `{zone, tags, capacity}`), `stagedParameters` |
| `UpdateClusterLayout` | `{"roles": [{"id", "zone", "tags", "capacity"}] or [{"id", "remove": true}], "parameters": {"zoneRedundancy": ...} or null}` | the `GetClusterLayout` answer, with the change staged |
| `PreviewClusterLayoutChanges` | none | `{"message": [lines], "statistics": {...}, "newLayout": {...}}` or `{"error": "..."}` when it cannot be computed |
| `ApplyClusterLayout` | `{"version": N}` where N = current version + 1 | `message[]`, `statistics`, `layout` |
| `RevertClusterLayout` | none | the layout, staging cleared |
| `GetClusterLayoutHistory` | none | `currentVersion`, `minAck`, `versions[]` (`version`, `status` `Current`/`Draining`/`Historical`, `storageNodes`, `gatewayNodes`), `updateTrackers` `{nodeId: {ack, sync, syncAck}}` or null |
| `ClusterLayoutSkipDeadNodes` | `{"version": N, "allowMissingData": bool}` | `ackUpdated[]`, `syncUpdated[]` |
| `GetClusterStatus` | none | `layoutVersion`, `nodes[]` (`id`, `hostname`, `isUp`, `role` or null, `draining`, `dataPartition`) |

`zoneRedundancy` is `{"atLeast": N}` or the string `"maximum"`. `statistics`
(`ComputationStat`) carries `replicationFactor`, `effectiveZoneRedundancy`, `partitionSize`,
`usableCapacity`, `totalCapacity`, `effectiveCapacity`, `lowPartitionSize`,
`lowUsableCapacity`, `totalMovedPartitions`, `zones`: enough to show what an apply costs.

The CLI's own `layout assign` (`src/garage/cli/remote/layout.rs`) is the reference for the
rules the editor must enforce: a node new to the layout needs a zone; it needs a capacity
**or** the gateway flag, never both; tags default to the current role's; a role change on a node
already in the layout keeps the fields left empty. The node id must be the full id (the app has
it from `GetClusterStatus`, no prefix matching needed). Garage refuses `ApplyClusterLayout`
when the version is not exactly current + 1: passing the version the preview returned makes
two phones applying at once harmless.

## Proposed Go facade (`go/ichorgo`)

One file `kube_garage_layout.go`, with the same guards as the other Garage actions
(`withGarage`, `validateKubeName`, demo refusal, `recordAction`, `kubeMutationError`,
`maskResult`).

- `garageAPI[T](ctx, run, t, endpoint, body)`: cluster-wide call, argv
  `["/garage", "json-api", endpoint, json(body)]` (no third argument when `body` is nil),
  decoding stdout into `T`. Reads (`GetClusterLayout`, `GetClusterLayoutHistory`,
  `PreviewClusterLayoutChanges`) join `garageReads` so a cut answer is asked again.
- `KubeGarageLayout(cfg, ctx, server, ns, pod) → garageLayoutReport`: runs `GetClusterLayout`,
  `GetClusterLayoutHistory` and `GetClusterStatus` in parallel, then
  `PreviewClusterLayoutChanges` only when something is staged. It merges them into one list the
  UI can draw without joins:

  ```
  version, partitionSize, zoneRedundancy ("maximum" | "3"), stagedZoneRedundancy
  nextVersion            // version + 1 when something is staged, else 0
  preview: { ok, messages[], error, movedPartitions, usableCapacity, effectiveCapacity, lowUsableCapacity }
  nodes[]: { id, hostname, kubeNode, up, draining, dataTotal,
             role: { zone, capacity (-1 = gateway), tags[], storedPartitions, usableCapacity } | null,
             staged: "none" | "add" | "change" | "remove",
             stagedRole: { zone, capacity, tags[] } | null }
  versions[]: { version, status, storageNodes, gatewayNodes }
  minAck, trackers[]: { id, hostname, ack, sync, syncAck }   // only when several versions are live
  stuck: bool            // a draining version whose trackers point at a node that is down
  ```

  Nodes known to the cluster but without a role appear with `role: null`: they are the
  candidates for "Add to layout". A role whose node `GetClusterStatus` no longer knows (a
  machine gone for good) still appears, with its id as the label.
- `KubeGarageStageRole(cfg, ctx, server, ns, pod, nodeID, zone, capacityBytes, gateway, tagsJSON)`:
  validates (`garageNodeIDPattern`, zone non-empty and `^[A-Za-z0-9._-]{1,64}$`, `capacity > 0`
  xor `gateway`, each tag same pattern), sends one `UpdateClusterLayout` role, and returns the
  report above (so the sheet shows the preview in the same round trip). Audit:
  `garage-layout-stage` with `node=<id16> zone=… capacity=… gateway=… tags=…`.
- `KubeGarageStageRemoval(..., nodeID)`: `{"id", "remove": true}`; returns the report.
- `KubeGarageSetZoneRedundancy(..., value string)`: `"maximum"` or an integer; parameters only.
- `KubeGarageApplyLayout(..., version int64) → { messages[], layout report }`. The app passes
  the `nextVersion` it displayed; Garage refuses a stale one.
- `KubeGarageRevertLayout(...)`.
- `KubeGarageSkipDeadNodes(..., version, allowMissingData)`: the escape hatch when a draining
  layout waits on a node that will never come back. Optional, see phasing.
- `go/cmd/probe`: `garage-layout`, `garage-layout-stage`, `garage-layout-remove`,
  `garage-layout-apply`, `garage-layout-revert` for trying it against a real cluster.
- Tests extend `fakeGarageAPI`: it currently rejects any argv that is not `garageCommands.status`
  or a 4-item node-scoped call; it gains the cluster-wide shape (3 or 4 items, no envelope) and
  canned answers copied from the spec. Cases: merge of a node with no role, staged add/change/
  remove, preview error, apply with the wrong version (the CLI error line), revert, history
  with trackers.
- Demo: `demoGarageLayout()` for the demo context, with one staged change so the UI path is
  visible offline; writes keep answering `errDemoUnavailable`.

## Models

`app/src/main/java/name/levis/ichor/model/GarageLayout.kt` and
`ios/IchorCore/Sources/IchorCore/GarageLayout.swift` mirror the report, with a decoding test on
each platform fed by the Go demo answer (as `GarageMaintenanceTest` does). Capacity is formatted
with the existing `formatBytes`; the editor parses "500 GB" / "1.5 TB" style input into bytes
with a small shared helper (Garage itself uses IEC units in its CLI output; the editor offers a
unit picker so nothing is parsed from free text).

## UI

Same structure on Android and iOS, built from the existing components.

### In the instance card

- A **Layout row** between the tiles and the nodes: `Layout v12 · 3 storage · 1 gateway ·
  zone redundancy maximum`. A warn `Tag` "1 change staged" when `nextVersion > 0`; a bad `Tag`
  "migration stuck" when `stuck`. Tapping opens the Layout sheet. (The `layoutVersion` row goes
  into "Details" too, for completeness.)
- **Node rows**: the subtitle gains the role (`zone-1 · 1.8 TB · 92% used by layout` or
  `gateway`), a muted `Tag` "no role" for a known node outside the layout, and a `Tag` "staged"
  when a change waits for it. The ⋮ menu (iOS: swipe and context menu) gains, after the
  tranquility item: **Add to layout…** (no role), **Change role…** (has a role), **Remove from
  layout** (has a role, stages a removal). These stage only; applying is always done from the
  sheet, where the preview is.

### Layout sheet (`GarageLayoutSheet` / `GarageLayoutView`)

A sheet like `GarageBlocksSheet`, reloadable, with its state in `GarageActions`.

1. **Tiles**: version, partition size, usable capacity (with effective capacity under it).
2. **Staged changes** (only when there are): one `ResourceRow` per change, coloured by kind
   (add ok, change warn, remove bad) with the before → after role in the subtitle; the preview's
   `message` lines as an `InfoHint` (they are Garage's own words, e.g. partitions moved per zone),
   or the preview `error` as an inline section error with Revert as the only way out. Two
   buttons: **Apply version 13** (the one filled button of the sheet, typed confirmation: the
   user types the new version number, since an apply is cluster-wide and moves data) and
   **Revert** (plain confirmation).
3. **Roles**: one row per node with a role, led by its `HealthDot` (down nodes first as the
   nodes list already does): zone and tags as `Tag`s, capacity, and a `MeterRow` of
   `usableCapacity / capacity` so a node whose disk the layout cannot use stands out
   (`lowUsableCapacity` explained in an `InfoHint`). Row menu: Change role…, Remove from layout.
4. **Nodes without a role**: the candidates, with **Add to layout…** in the row. Empty when all
   known nodes have one.
5. **History** (collapsed by default): a row per version (`#12 · Current · 3 storage · 1
   gateway`, `#11 · Draining …`), then `minAck`; when several versions are live, the trackers
   per node (ack / sync / syncAck) and, if `stuck`, an `InfoHint` naming the down node with the
   **Skip dead nodes** action (typed confirmation, destructive colour; the `allowMissingData`
   variant behind a second explicit switch in the dialog, worded as data loss).
6. **Parameters**: zone redundancy with a small picker (maximum / at least N); staging it goes
   through the same preview and apply.

### Role editor (`GarageRoleSheet`)

Opened by "Add to layout…" and "Change role…", prefilled from the current or staged role:

- **Zone**: a text field with the cluster's existing zones as suggestions (chips); required.
- **Role**: a two-way choice, *Storage* or *Gateway*. Storage shows **Capacity**: a number
  field and a unit picker (GB / TB, IEC under the hood as Garage's CLI prints), prefilled from
  the node's data partition total rounded down, with the disk size shown as a hint ("disk 1.9 TB,
  Garage recommends slightly less than the disk"). Gateway hides it.
- **Tags**: chips, add and remove; prefilled with the current tags, or the Kubernetes node name
  for a new node (which is what the demo cluster and most charts do).
- Inline validation: zone required, capacity > 0, nothing else.
- Primary button **Stage change**. On success the sheet closes and the Layout sheet shows the
  staged row with its preview and the Apply button: add → preview → apply is three taps, each
  showing what the next one does. A failure shows Garage's error line in the sheet.

Outcomes go through `GarageResultToasts` ("Role staged for garage-2xv9p", "Layout version 13
applied", "Staged changes reverted", or the error).

### garage-operator managed clusters

When a `GarageCluster` CR lives in the instance's namespace and its pods carry the CR's name
(today the operator layer is only listed beside the instances; the Go report gains a
`managedBy` field doing that match), the operator owns the layout: staging through the CLI would be reverted or
race with it. The Layout sheet then shows the roles and history read-only with an `InfoNotice`
"Managed by garage-operator: edit the GarageNode objects", and the node menu's **Add to
layout…** becomes **Open GarageNode** through the existing `onObject`, or, for a node with no
CR yet, is hidden. The operator's own `stagedLayoutVersion` already shows in the operator card.

### Permissions and safety

- RBAC: these actions need nothing beyond `pods/exec`, already what the Garage actions assume;
  `rememberKubeDenial(KubeAction.EXEC_POD, namespace)` hides the menu items up front, as the
  pod shell does.
- Confirmations follow CONTRIBUTING: staging and revert are reversible (plain), apply and skip
  dead nodes are cluster-wide (typed).
- Every write lands in the audit log with the node and the role; the preview's
  `totalMovedPartitions` is shown in the apply dialog so the user knows how much data will move.
- Garage below 2.0 has no `json-api`: such a cluster already reads as `health` only
  (`detailed == false`) and shows no action, nothing new to gate.

## Phasing

1. **Read-only** (small): `KubeGarageLayout`, models, the Layout row, the sheet with roles,
   staged changes, preview and history. Already answers "which version, who is in, is a
   migration stuck".
2. **Edit** (medium): stage role / removal, revert, apply, the role editor, toasts, audit, demo
   staging, operator read-only mode, translations (de, es, fr, it, uk), features.md.
3. **Advanced** (small, optional): zone redundancy parameter, skip dead nodes.

Rough size: Go ~500 lines with tests, Kotlin ~700 (model, actions, two sheets, strings), Swift
~600, plus the five string translations.

## Open points

- The typed confirmation token for apply: the new version number (short, and it is exactly what
  Garage's CLI asks for) or the instance name as elsewhere in the app.
- Whether to expose the gateway role at all in the editor, or keep it storage-only at first.
- Whether phase 3's skip-dead-nodes belongs in a phone app, or should stay a documented
  `garage layout skip-dead-nodes` with the app only naming the stuck node.
