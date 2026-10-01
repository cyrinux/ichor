import SwiftUI

struct MonitoringSection: View {
    @State private var enabled = BackgroundMonitor.alertsEnabled
    @State private var message: String?

    var body: some View {
        Section {
            Toggle("Background alerts", isOn: Binding(get: { enabled }, set: { set($0) }))
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
            Text("Notifies when a node goes down or recovers, on new etcd alarms, and daily when the client certificate expires within \(14) days. iOS decides when background checks run, so alerts can be delayed. The home-screen widget shows the last check.")
        }
    }

    private func set(_ on: Bool) {
        Task {
            if on, !(await BackgroundMonitor.requestPermission()) {
                message = String(localized: "Notifications are off for Talosdev Mobile: allow them in the Settings app.")
                return
            }
            BackgroundMonitor.alertsEnabled = on
            enabled = on
            if on { BackgroundMonitor.schedule() }
        }
    }
}
