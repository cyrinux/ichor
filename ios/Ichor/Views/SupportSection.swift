import SwiftUI
import IchorCore

/// Support bundle (any role), integration requests (os:admin: they read the API groups) and
/// bug reports.
struct SupportSection: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        Section {
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
                    Text("Every change Ichor made to your clusters (reboots, scaling, syncs, rollbacks…), kept 90 days on this device.")
                        .font(.caption).foregroundStyle(.secondary)
                }
            }
            Link(destination: IntegrationLinks.reportBug) { Label("Report a bug", systemImage: "ladybug") }
        } header: {
            Text("Support")
        }
    }
}
