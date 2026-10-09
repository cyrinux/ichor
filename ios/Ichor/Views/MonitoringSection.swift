import SwiftUI
import IchorCore

struct MonitoringSection: View {
    @Environment(AppModel.self) private var model
    @State private var enabled = BackgroundMonitor.alertsEnabled
    @State private var dataWatched = BackgroundMonitor.dataServicesWatched
    @State private var gitopsWatched = BackgroundMonitor.gitopsWatched
    @State private var checkupWatched = BackgroundMonitor.checkupWatched
    @State private var alertmanagerWatched = BackgroundMonitor.alertmanagerWatched
    @State private var unreachableWatched = BackgroundMonitor.unreachableWatched
    @State private var unreachableRuns = BackgroundMonitor.unreachableRuns
    @State private var message: String?

    var body: some View {
        Section {
            Toggle("Background alerts", isOn: Binding(get: { enabled }, set: { set($0) }))
            // Opt-in on top of the alerts: Kubernetes API calls and a Garage CLI run at every check.
            Toggle(isOn: Binding(get: { dataWatched }, set: { on in
                BackgroundMonitor.dataServicesWatched = on
                dataWatched = on
            })) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Watch data services")
                    Text("Also alerts on Longhorn volumes that fault or degrade, a degraded Garage cluster and Postgres clusters that go down or whose backups fail. Each check uses the Kubernetes API (os:admin) and runs the Garage CLI in one of its pods.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            .disabled(!enabled)
            // Opt-in too: lists the Argo CD Applications and Flux objects at every check.
            Toggle(isOn: Binding(get: { gitopsWatched }, set: { on in
                BackgroundMonitor.gitopsWatched = on
                gitopsWatched = on
            })) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Watch GitOps apps")
                    Text("Argo CD and Flux apps that break or fail to sync; each check reads them through the Kubernetes API.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            .disabled(!enabled)
            // Opt-in too: the checkup lists every pod and asks each kubelet at every check.
            Toggle(isOn: Binding(get: { checkupWatched }, set: { on in
                BackgroundMonitor.checkupWatched = on
                checkupWatched = on
            })) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: CheckupText.monitorCheckup)
                    Text(verbatim: CheckupText.monitorCheckupDesc)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            .disabled(!enabled)
            // Opt-in too: asks the cluster's Alertmanager for its alerts at every check.
            Toggle(isOn: Binding(get: { alertmanagerWatched }, set: { on in
                BackgroundMonitor.alertmanagerWatched = on
                alertmanagerWatched = on
            })) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Watch Alertmanager alerts")
                    Text("Critical alerts at once, warnings seen on two checks in a row, and when they resolve; silenced, inhibited and info alerts never notify. Each check asks the cluster's Alertmanager through the Kubernetes API or its URL.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            .disabled(!enabled)
            // Opt-in too: no extra call, only the runs a cluster did not answer in a row.
            Toggle(isOn: Binding(get: { unreachableWatched }, set: { on in
                BackgroundMonitor.unreachableWatched = on
                unreachableWatched = on
            })) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Alert when a cluster is unreachable")
                    Text("When a cluster does not answer several checks in a row (off its network, its API down), and once it answers again. Its node alerts stay silent meanwhile.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            .disabled(!enabled)
            if unreachableWatched {
                Stepper(value: Binding(get: { unreachableRuns }, set: { runs in
                    BackgroundMonitor.unreachableRuns = runs
                    unreachableRuns = runs
                }), in: unreachableRunsRange) {
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
            // Off: what was kept is deleted (AppModel.setKeepLastKnown).
            Toggle(isOn: Binding(get: { model.keepLastKnown }, set: { model.setKeepLastKnown($0) })) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Keep last known state")
                    Text("Save the last data fetched from each cluster on this phone, encrypted, so it still shows when the cluster can't be reached. Kept for 24 hours and never backed up.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
        } header: {
            Text("Monitoring")
        } footer: {
            VStack(alignment: .leading, spacing: 4) {
                Text("Notifies when a node goes down or recovers, on new etcd alarms, and daily when the client certificate expires within \(certWarnDays) days. iOS schedules background checks itself, so there is no check interval to choose as on Android, and alerts can be delayed. The home-screen widget shows the last check.")
                Text("Each check covers every cluster. To leave one out, turn off Watch in the background in its menu under Clusters.")
                // The sealed configs cannot be read in the background after iOS closed the app.
                if model.requiresKey { Text("A security key is required: alerts and the widget pause when iOS has closed Ichor, until you unlock it again.") }
                // README "Widget": the App Group a sideloaded build may lack with a free Apple ID.
                if !Distribution.appStore { Text("The widget reads the last check through an App Group, which a free Apple ID may not allow when sideloading. In that case the widget stays empty.") }
            }
        }
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
