import SwiftUI
import IchorCore

/// Support bundle (any role), integration requests (os:admin: they read the API groups) and
/// bug reports.
struct SupportSection: View {
    @Environment(AppModel.self) private var model
    @AppStorage(StartupTrace.defaultsKey) private var startupTrace = false
    /// The bug forms in the in-app browser (SafariView says why not a Link).
    @State private var browsing: URL?
    /// Why the action log and the node names are not saved (nil while they are).
    @State private var dataStoreProblem: String?

    var body: some View {
        Section {
            if let dataStoreProblem {
                Label(
                    "Action log and node names are not being saved: \(dataStoreProblem)",
                    systemImage: "exclamationmark.triangle"
                )
                .font(.footnote)
                .foregroundStyle(.orange)
            }
            if model.allows(.supportBundle) {
                let support = model.clusterSupport(.supportBundle)
                NavigationLink(value: Route.supportBundle) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("Create a support bundle…")
                        Text("Collect logs and cluster details of the nodes into a zip, like talosctl support.")
                            .font(.caption).foregroundStyle(.secondary)
                        if let notice = support.localizedNotice {
                            Text(notice).font(.caption).foregroundStyle(.secondary)
                        }
                    }
                }
                .disabled(!support.supported)
            }
            if model.allows(.workloads) {
                NavigationLink(value: Route.integrations) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("Request an integration…")
                        Text("Ask for an operator this cluster runs that Ichor does not show yet.")
                            .font(.caption).foregroundStyle(.secondary)
                    }
                }
            }
            // On this device only, whatever the cluster: no role needed.
            NavigationLink(value: Route.activity(cluster: "")) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Activity log")
                    Text(
                        "Every change Ichor made to your clusters (reboots, scaling, syncs, rollbacks…), kept 90 days on this device."
                    )
                    .font(.caption).foregroundStyle(.secondary)
                }
            }
            Button {
                browsing = IntegrationLinks.reportBug
            } label: {
                Label("Report a bug", systemImage: "ladybug")
            }
            Toggle(isOn: $startupTrace) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Startup timing in the log")
                    Text(
                        "Logs how long each step of a cold start takes (config, cluster connection, first node, home screen), for a bug report: Console, category startup. Durations and counts only, never a cluster name or an address. Stays on the device."
                    )
                    .font(.caption)
                    .foregroundStyle(.secondary)
                }
            }
            .onChange(of: startupTrace) { _, on in TalosClient.setStartupTrace(on) }
        } header: {
            Text("Support")
        }
        .safariSheet($browsing)
        .task { dataStoreProblem = TalosClient.dataStoreStatus()?.problem }
    }
}
