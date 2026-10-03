# Phase 2: Android

Prerequisite: phase 1 merged (the `Talosmobile.kubeDataServices` binding exists).
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
        TalosJson.decodeFromString(DataServices.serializer(), Talosmobile.kubeDataServices(cfg, ctx, server, hints))
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
    (`scheduled/maximum`).
  - `GarageTab.kt`: one card per instance with a big status, the message, stat rows
    (storage nodes ok/total, partitions quorum/all-ok/total, connected/known nodes,
    resync queue, resync errors in red when > 0). Failed nodes are listed with their
    last-seen age, and staged layout changes get a warning chip. When
    `source == "health"`: an `InfoNotice` with the `message` (for example "exec refused")
    explaining why only the basic status is shown. When `source == "cli-text"`: an
    expandable "Details" block with `raw` in a monospace font.
  - `CnpgTab.kt`: rows per cluster: `ns/name`, a phase chip, `ready/instances`, the primary
    (with "→ target" when switching), archiving and backup badges, last backup age, and the
    first recoverability point.
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

- [ ] `just test` and `just i18n-check` green
- [ ] Demo cluster shows the card and all three tabs with every state (degraded volume,
      switchover, resync queue)
- [ ] A cluster without the three operators: no card, and no Kubernetes call made (check
      with `just logs`: no kubeconfig fetched on Overview load)
- [ ] A non-admin context: no card
- [ ] Real cluster checked by the user
