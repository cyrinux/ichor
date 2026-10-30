import SwiftUI
import IchorCore

struct MonitoringSection: View {
    @Environment(AppModel.self) private var model
    @State private var enabled = BackgroundMonitor.alertsEnabled
    @State private var dataWatched = BackgroundMonitor.dataServicesWatched
    @State private var importedPlugins = BackgroundMonitor.importedPluginAlerts
    @State private var gitopsWatched = BackgroundMonitor.gitopsWatched
    @State private var checkupWatched = BackgroundMonitor.checkupWatched
    @State private var alertmanagerWatched = BackgroundMonitor.alertmanagerWatched
    @State private var storageWatched = BackgroundMonitor.storageWatched
    @State private var storageThresholds = BackgroundMonitor.storageThresholds
    @State private var storageTrendWatched = BackgroundMonitor.storageTrendWatched
    @State private var sensorsWatched = BackgroundMonitor.sensorsWatched
    @State private var sensorThreshold = BackgroundMonitor.sensorThreshold
    @State private var unreachableWatched = BackgroundMonitor.unreachableWatched
    @State private var unreachableRuns = BackgroundMonitor.unreachableRuns
    @State private var message: String?

    var body: some View {
        Section {
            Toggle("Background alerts", isOn: Binding(get: { enabled }, set: { set($0) }))
            // Opt-in on top of the alerts: Kubernetes API calls and a Garage CLI run at every check.
            Toggle(
                isOn: Binding(
                    get: { dataWatched },
                    set: { on in
                        BackgroundMonitor.dataServicesWatched = on
                        dataWatched = on
                    })
            ) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Watch data services")
                    Text(
                        "Also alerts on Longhorn volumes that fault or degrade, a degraded Garage cluster and Postgres clusters that go down or whose backups fail. Each check uses the Kubernetes API (os:admin) and runs the Garage CLI in one of its pods."
                    )
                    .font(.caption)
                    .foregroundStyle(.secondary)
                }
            }
            .disabled(!enabled)
            // Imported plugins: their findings notify only once this is on.
            Toggle(
                isOn: Binding(
                    get: { importedPlugins },
                    set: { on in
                        BackgroundMonitor.importedPluginAlerts = on
                        importedPlugins = on
                    })
            ) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Alerts from imported plugins")
                    Text(
                        "Plugins you imported notify too, like the built-in ones. Off: only built-in plugins send alerts."
                    )
                    .font(.caption)
                    .foregroundStyle(.secondary)
                }
            }
            .disabled(!enabled)
            // Opt-in too: lists the Argo CD Applications and Flux objects at every check.
            Toggle(
                isOn: Binding(
                    get: { gitopsWatched },
                    set: { on in
                        BackgroundMonitor.gitopsWatched = on
                        gitopsWatched = on
                    })
            ) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Watch GitOps apps")
                    Text(
                        "Argo CD and Flux apps that break or fail to sync; each check reads them through the Kubernetes API."
                    )
                    .font(.caption)
                    .foregroundStyle(.secondary)
                }
            }
            .disabled(!enabled)
            // Opt-in too: the checkup lists every pod and asks each kubelet at every check.
            Toggle(
                isOn: Binding(
                    get: { checkupWatched },
                    set: { on in
                        BackgroundMonitor.checkupWatched = on
                        checkupWatched = on
                    })
            ) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: CheckupText.monitorCheckup)
                    Text(verbatim: CheckupText.monitorCheckupDesc)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            .disabled(!enabled)
            // Opt-in too: asks the cluster's Alertmanager for its alerts at every check.
            Toggle(
                isOn: Binding(
                    get: { alertmanagerWatched },
                    set: { on in
                        BackgroundMonitor.alertmanagerWatched = on
                        alertmanagerWatched = on
                    })
            ) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Watch Alertmanager alerts")
                    Text(
                        "Critical alerts at once, warnings seen on two checks in a row, and when they resolve; silenced, inhibited and info alerts never notify. Each check asks the cluster's Alertmanager through the Kubernetes API or its URL."
                    )
                    .font(.caption)
                    .foregroundStyle(.secondary)
                }
            }
            .disabled(!enabled)
            // Opt-in too: one Talos call per check reads every node's volumes and disks.
            Toggle(
                isOn: Binding(
                    get: { storageWatched },
                    set: { on in
                        BackgroundMonitor.storageWatched = on
                        storageWatched = on
                    })
            ) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Watch node storage")
                    Text(
                        "Volumes such as EPHEMERAL filling up past the thresholds below and disks failing SMART on Talos clusters, read through the Talos API; each node's disk and DiskPressure on clusters added from a kubeconfig, read from its kubelet."
                    )
                    .font(.caption)
                    .foregroundStyle(.secondary)
                }
            }
            .disabled(!enabled)
            if storageWatched {
                storageSteppers.disabled(!enabled)
                // Opt-in: no extra call, a line through each volume's fill in the history kept on this phone.
                Toggle(
                    isOn: Binding(
                        get: { storageTrendWatched },
                        set: { on in
                            BackgroundMonitor.storageTrendWatched = on
                            storageTrendWatched = on
                        })
                ) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("Warn before a volume fills up")
                        Text(
                            "When a volume's growth over the last week would make it critical within 3 days, before it crosses the thresholds above."
                        )
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    }
                }
                .disabled(!enabled)
            }
            // Opt-in too: one Talos call per check reads every node's hottest sensor.
            Toggle(
                isOn: Binding(
                    get: { sensorsWatched },
                    set: { on in
                        BackgroundMonitor.sensorsWatched = on
                        sensorsWatched = on
                    })
            ) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Node temperatures")
                    Text(
                        "Alert when a node runs hot or its cores are throttled (Talos clusters; reads every node's sensors at each check)."
                    )
                    .font(.caption)
                    .foregroundStyle(.secondary)
                }
            }
            .disabled(!enabled)
            if sensorsWatched {
                Toggle(
                    "Each sensor's own limit (critical − 5 °C, else its max)",
                    isOn: Binding(
                        get: { sensorThreshold == sensorThresholdAuto },
                        set: { auto in
                            setSensorThreshold(auto ? sensorThresholdAuto : sensorThresholdDefault)
                        })
                )
                .disabled(!enabled)
                if sensorThreshold != sensorThresholdAuto {
                    Stepper(
                        value: Binding(get: { sensorThreshold }, set: { setSensorThreshold($0) }),
                        in: sensorThresholdRange
                    ) {
                        Text("Alert at \(sensorThreshold) °C")
                    }
                    .disabled(!enabled)
                }
            }
            // Opt-in too: no extra call, only the runs a cluster did not answer in a row.
            Toggle(
                isOn: Binding(
                    get: { unreachableWatched },
                    set: { on in
                        BackgroundMonitor.unreachableWatched = on
                        unreachableWatched = on
                    })
            ) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Alert when a cluster is unreachable")
                    Text(
                        "When a cluster does not answer several checks in a row (off its network, its API down), and once it answers again. Its node alerts stay silent meanwhile."
                    )
                    .font(.caption)
                    .foregroundStyle(.secondary)
                }
            }
            .disabled(!enabled)
            if unreachableWatched {
                Stepper(
                    value: Binding(
                        get: { unreachableRuns },
                        set: { runs in
                            BackgroundMonitor.unreachableRuns = runs
                            unreachableRuns = runs
                        }), in: unreachableRunsRange
                ) {
                    Text("After \(unreachableRuns) checks in a row")
                }
                .disabled(!enabled)
            }
            Button("Check now") {
                Task {
                    await BackgroundMonitor.check()
                    message = String(localized: "Checked. The widget is updated.")
                }
            }
            if let message { Text(message).font(.footnote).foregroundStyle(.secondary) }
        } header: {
            Text("Alerts")
        } footer: {
            VStack(alignment: .leading, spacing: 4) {
                Text(
                    "Notifies when a node goes down or recovers, on new etcd alarms, and daily when the client certificate expires within \(certWarnDays) days. iOS schedules background checks itself, so there is no check interval to choose as on Android, and alerts can be delayed. The home-screen widget shows the last check."
                )
                Text(
                    "Each check covers every cluster. To leave one out, turn off Watch in the background in its menu under Clusters."
                )
                // The sealed configs cannot be read in the background after iOS closed the app.
                if model.requiresKey {
                    Text(
                        "A security key is required: alerts and the widget pause when iOS has closed Ichor, until you unlock it again."
                    )
                }
                // README "Widget": the App Group a sideloaded build may lack with a free Apple ID.
                if !Distribution.appStore {
                    Text(
                        "The widget reads the last check through an App Group, which a free Apple ID may not allow when sideloading. In that case the widget stays empty."
                    )
                }
            }
        }
    }

    /// The warning and critical fill thresholds; critical stays above warning.
    @ViewBuilder private var storageSteppers: some View {
        let warnLabel = "\(storageThresholds.warn) %"
        let critLabel = "\(storageThresholds.crit) %"
        Stepper(
            value: Binding(
                get: { storageThresholds.warn },
                set: { warn in
                    setStorage(StorageThresholds(warn: warn, crit: max(storageThresholds.crit, warn + 1)))
                }), in: storageWarnRange
        ) {
            Text("Warning from \(warnLabel) used")
        }
        Stepper(
            value: Binding(
                get: { storageThresholds.crit },
                set: { crit in
                    setStorage(StorageThresholds(warn: storageThresholds.warn, crit: crit))
                }), in: storageCritRange(warn: storageThresholds.warn)
        ) {
            Text("Critical from \(critLabel) used")
        }
    }

    private func setStorage(_ thresholds: StorageThresholds) {
        BackgroundMonitor.storageThresholds = thresholds
        storageThresholds = thresholds
    }

    private func setSensorThreshold(_ celsius: Int) {
        BackgroundMonitor.sensorThreshold = celsius
        sensorThreshold = BackgroundMonitor.sensorThreshold
    }

    private func set(_ on: Bool) {
        Task {
            if on, !(await BackgroundMonitor.requestPermission()) {
                message = String(localized: "Notifications are off for Ichor: allow them in the Settings app.")
                return
            }
            BackgroundMonitor.alertsEnabled = on
            enabled = on
            if on { BackgroundMonitor.schedule() }
        }
    }
}
