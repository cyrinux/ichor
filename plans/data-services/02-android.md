# Phase 2: Android

Prerequisite: phase 1 merged (the `Ichorgo.kubeDataServices` binding exists).
Paths are relative to `app/src/main/java/name/levis/ichor/`.

## 1. Model: `model/DataServices.kt` (+ `app/src/test/.../model/DataServicesTest.kt`)

- `@Serializable` classes mirroring the wire format in the README: `DataServices`,
  `LonghornStatus`, `LonghornVolume`, `LonghornNode`, `LonghornDisk`, `GarageStatus`,
  `GarageInstance`, `CnpgStatus`, `CnpgCluster`. Every field gets a default (pattern:
  `model/Workloads.kt`).
- Enums with `from(wire)` and an UNKNOWN fallback (pattern: `WorkloadState.from`):
  `ServiceHealth { OK, WARNING, CRITICAL, IDLE, UNKNOWN }`, `GarageHealth`,
  `ArchivingState`, `BackupState`.
- Pure helpers, unit tested:
  - `DataServices.detected: List<DataServiceKind>` (`LONGHORN`, `GARAGE`, `CNPG`)
  - `DataServices.summary(kind): ServiceSummary(total, warning, critical)` for the Overview card
  - `DataServices.worst: ServiceHealth`
  - `Inventory.dataServiceHints(): String` → `"longhorn,garage"` from `InventoryApp.id`
    (ids `longhorn`, `garage`, `cloudnative-pg`)

## 2. Repository: `data/TalosRepository.kt`

```kotlin
suspend fun dataServices(hints: String): DataServices = remember(DATA_SERVICES) {
    kubeCall { cfg, ctx, server ->
        TalosJson.decodeFromString(DataServices.serializer(), Ichorgo.kubeDataServices(cfg, ctx, server, hints))
    }
}
```

Add `DATA_SERVICES` next to the `WORKLOADS`/`PODS` constants (~line 502).

## 3. ViewModel: `ui/dataservices/DataServicesViewModel.kt`

`class DataServicesViewModel(talos) : LoadingViewModel<DataServices>()` with `cached()`
and `fetch()`, plus `load(key, hints)` like `AppsViewModel.load` (loads once per key,
never polled; manual refresh with pull-to-refresh).

## 4. Overview card: `ui/overview/DataServicesCard.kt`

- In `OverviewScreen.kt`, right after `AppsCard` (line ~408), add `DataServicesCard`.
- Shown **only when** `activeSummary.allows(Feature.WORKLOADS)` **and** the inventory
  (already loaded for `AppsCard`) has a non-empty `dataServiceHints()`. Then
  `viewModel(key = "overview-data-services")` loads with those hints.
- Layout: a `Section`-style card, one row per detected system:
  icon (`AppIcon` with catalog ids `longhorn`, `garage`, `postgresql`), name, one-line
  summary, and a status dot (use the existing health colours, see `WorkloadState.color()`).
  - Longhorn: "12 volumes · 1 degraded" / "all healthy"
  - Garage: "Healthy · 256/256 partitions" / "Degraded: 2 of 3 storage nodes" / "Resync: 12 queued"
  - Postgres: "3 clusters · all ready" / "pg: 2/3 ready, switchover"
- Loading: a skeleton row per hinted system. Failure: a single muted line with a retry
  icon, never a big `ErrorBox` on the Overview.
- Tap → `onDataServices()` → `Routes.DATA_SERVICES`.

## 5. Screen: `ui/dataservices/DataServicesScreen.kt` (+ per-tab files)

- Route `DATA_SERVICES = "data-services"` in `ui/Navigation.kt` `Routes`, with a `composable`
  next to `Routes.WORKLOADS` (~line 419), and an `onDataServices` lambda passed to the Overview.
- Scaffold + TopAppBar (title `data_services_title`), with a refresh action and the same
  kube-server button as `KubernetesScreen` (reuse `KubeServerDialog`; after save, refresh
  this VM).
- `PrimaryTabRow` showing only detected systems (if just one, no tab row).
- Files, each under ~300 lines:
  - `LonghornTab.kt`: a filter chip row (All / Problems / Detached) and a `LazyColumn` of
    `VolumeRow`: PVC `ns/name` (or volume name when unbound), state + robustness chip,
    replicas `2/3` with a rebuilding badge, node, `actualSize / size` (`util/Format.kt`
    bytes formatter), last backup age (`ui/components/Durations.kt`). Below that, a
    "Nodes" section: each node's ready/schedulable state and disks with a usage bar
    (`scheduled/maximum`). At the top, a backup-target row (available or not, with its
    message). When it's unavailable and Garage is detected, link to the Garage tab.
  - `GarageTab.kt`: one card per instance (the user has two: a
    multi-node cluster and a standalone one), titled `namespace/name`, with a big status, the message, stat rows
    (storage nodes ok/total, partitions quorum/all-ok/total, connected/known nodes,
    resync queue, resync errors in red when > 0). Below that, a node list (hostname,
    zone, up/down with last-seen age, data disk usage bar, per-node resync queue/errors).
    Staged layout changes get a warning chip. When
    `source == "health"`: an `InfoNotice` with the `message` (for example "exec refused")
    explaining why only the basic status is shown. When `source == "cli-text"`: an
    expandable "Details" block with `raw` in a monospace font.
  - `CnpgTab.kt`: built for many clusters (the user has 29):
    - A summary header: "29 clusters · 27 ok · 2 need attention".
    - Filter chips: Problems (the default when any exist) / All.
    - Compact rows: `ns/name`, `ready/instances`, a health dot. Problems show their
      reason inline ("backup stale 12 d", "switchover pg-1 → pg-2").
    - Tapping a row expands it: phase, primary, archiving badge (`off` shown as a muted
      "no WAL archiving", not as an error), backup method + ObjectStore, last success and
      last failure ages, first recoverability point.
  - A section `error` → `ErrorBox` inside that tab only.
- Reuse `KubeFilters` (`ui/workloads/WorkloadsScreen.kt:171`) for namespace and search on
  the Longhorn and CNPG tabs.
- Hide the kube-server button in demo and screenshot mode, as `KubernetesScreen` does.

## 6. Strings: `res/values*/strings.xml`

Keys prefixed `data_services_*`, `longhorn_*`, `garage_*`, `cnpg_*`. They're **required** in
`values`, `values-fr`, `-de`, `-es`, `-it`, `-uk` (plurals: Ukrainian needs
one/few/many/other). `TranslationsTest` and `just i18n-check` enforce this. Keep product
names (Longhorn, Garage, CloudNativePG) untranslated.

## 7. Tests

- `model/DataServicesTest.kt`: decoding the README sample JSON, unknown enum values,
  summary/worst helpers, `dataServiceHints`.
- Compose UI tests aren't used in this repo; don't add a framework. Check visually with
  the demo cluster (`just build`, then open the demo) and `just screenshot data-services`.

## Done when

- [x] `./build.sh check` (Kotlin compile + unit tests incl. `DataServicesTest` 11/11, `TranslationsTest`)
      and `scripts/check-translations.py` green
- [ ] Demo cluster shows the card and all three tabs with every state (degraded volume,
      switchover, resync queue). **Not checked visually yet**: no device or emulator was
      attached to the dev session. Install with `just build` and open the demo.
- [x] A cluster without the three operators: no card and no Kubernetes call. By construction,
      the Overview only loads when `Inventory.dataServiceHints()` is non-empty.
- [x] A non-admin context: no card (gated on `Feature.WORKLOADS`).
- [ ] Real cluster checked by the user on the phone

Implementation notes (2026-10-03): the screen is `ui/dataservices/` (Screen, ViewModel, shared
`DataServicesUi.kt`, one file per tab) and the card is `ui/overview/DataServicesCard.kt`. Strings
are prefixed `data_services_`, `longhorn_`, `garage_` and `cnpg_`, and the summaries reuse
`apps_attention` and `pods_ready_count`. "Likely cause" (D9) is `DataServices.likelyCauses()`:
it uses Longhorn's not-ready nodes plus the Talos overview's not-ready hostnames.
