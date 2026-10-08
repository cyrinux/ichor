import SwiftUI
import IchorCore

struct MonitoringSection: View {
    @Environment(AppModel.self) private var model
    @State private var enabled = BackgroundMonitor.alertsEnabled
    @State private var dataWatched = BackgroundMonitor.dataServicesWatched
    @State private var gitopsWatched = BackgroundMonitor.gitopsWatched
    @State private var checkupWatched = BackgroundMonitor.checkupWatched
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
            Button("Check now") {
                Task {
                    await BackgroundMonitor.check()
                    message = String(localized: "Checked. The widget is updated.")
                }
            }
            if let message { Text(message).font(.footnote).foregroundStyle(.secondary) }
        } header: {
            Text("Monitoring")
        } footer: {
            Text("Notifies when a node goes down or recovers, on new etcd alarms, and daily when the client certificate expires within \(certWarnDays) days. iOS decides when background checks run, so alerts can be delayed. The home-screen widget shows the last check.")
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
