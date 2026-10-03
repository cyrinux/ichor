# Phase 4: background alerts

Prerequisites: phases 2 and 3. Alert on transitions only, like the existing etcd alarm
alerts.

## Behaviour

- New **opt-in** setting "Watch data services" in the Monitoring settings
  (`ui/settings/MonitoringSection.kt`, iOS `MonitoringSection.swift`), off by default.
  The hint text explains that it uses the Kubernetes API in the background.
- Alerts (each fires when the condition appears, and once when it clears):
  - Longhorn volume `faulted` → critical; `degraded` for 2 consecutive checks → warning
    (a single degraded check is often a short rebuild after a node reboot)
  - Garage status `degraded`/`unavailable`, or `resyncErrors > 0`
  - CNPG cluster `critical`, or `warning` caused by archiving failing or backup failed
    (*not* a switchover in progress, which is usually intentional)
- Keys: `longhorn:{ns}/{volume}`, `garage:{ns}/{svc}`, `cnpg:{ns}/{name}`, so the diff is
  stable.

## Android

1. `monitor/Snapshot.kt`: add `dataIssues: Map<String, String> = emptyMap()`
   (key → severity) and `dataChecked: Boolean = false`. These need defaults because the
   snapshot is persisted.
2. `monitor/MonitorWorker.kt`: when the setting is on and the context allows
   `Feature.WORKLOADS`, `runCatching { talosRepository.dataServices(hints = "") }`.
   Failure → `dataChecked = false` (blind check: no alert, same as etcd).
   Hints are `""` because the inventory isn't loaded in the background, and Go checks
   `/apis` anyway.
3. `monitor/Alerts.kt`: `AlertKind.DATA_SERVICE_PROBLEM`, `DATA_SERVICE_OK`. In `evaluate()`,
   diff the previous and current `dataIssues` exactly like `etcdAlarms`. Handle the
   "degraded twice" rule by storing a pending-degraded set in the snapshot.
4. `monitor/Notifications.kt:80-91`: titles and texts (translated).
5. `app/src/test/.../monitor/AlertsTest.kt`: baseline silent, appear, clear, blind check
   silent, degraded once silent, degraded twice alerts.

## iOS

Mirror it in `ios/IchorCore/Sources/IchorCore/Monitor.swift` (`snapshotOf`, `evaluate`),
`ios/Ichor/Services/BackgroundMonitor.swift`, and `MonitorTests.swift`.

## Cost note

Garage adds one or two short execs (`garage json-api GetClusterHealth`, plus the resync
counters) per run in one pod. That's cheap, but it shows up in the API server audit log
every 15 minutes; mention this in the setting's hint text.

Each background run in a fresh process fetches a new admin kubeconfig. Talos signs a new
client certificate per `Kubeconfig` call. That's fine at WorkManager's 15-minute minimum,
but it's the reason this is opt-in. A possible later improvement is to persist the
kubeconfig encrypted until it nears expiry; that's out of scope here.

## Done when

- [ ] `just test`, `just ios-test-linux` and `just i18n-check` green
- [ ] Manual check: on a test cluster, scale a Longhorn volume's replicas down or cordon a
      node, and see a notification within one monitor period
