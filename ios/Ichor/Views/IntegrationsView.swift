import SwiftUI
import IchorCore

/// The projects the app reads through the Kubernetes API, with their websites, and which ones the
/// active cluster runs: the list comes from the Go core, the status from one API discovery.
struct IntegrationsView: View {
    @Environment(AppModel.self) private var model
    @State private var list = TalosClient.integrations()
    @State private var problem: String?

    var body: some View {
        List {
            Section {
                ForEach(list.items) { item in
                    IntegrationRow(item: item, checked: list.checked)
                }
            } footer: {
                VStack(alignment: .leading, spacing: 4) {
                    Text("The projects Ichor reads through the Kubernetes API, found on the cluster from the API groups it serves. Tap one to open its website.")
                    if let problem { Text(verbatim: problem) }
                }
            }
        }
        .themedBackground()
        .navigationTitle("Integrations")
        .task(id: model.client?.kubeServer) { await load() }
    }

    private func load() async {
        guard model.allows(.workloads) else {
            problem = String(localized: "Detecting them on the cluster needs an os:admin config.")
            return
        }
        guard let client = model.client else { return }
        let hints = model.lastKnown(.inventory, as: ClusterInventory.self).map { integrationHints($0.value) } ?? ""
        do {
            list = try await client.integrations(hints: hints)
            problem = nil
        } catch is CancellationError {
        } catch {
            problem = error.localizedDescription
        }
    }
}

private struct IntegrationRow: View {
    let item: Integration
    let checked: Bool

    var body: some View {
        let row = HStack(spacing: 12) {
            AppIconView(app: item.app, size: 40)
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: item.name)
                Text(item.groups.isEmpty ? String(localized: "no API, found by its pods") : item.groups.joined(separator: ", "))
                    .font(.caption.monospaced())
                    .foregroundStyle(.secondary)
                if checked { status }
            }
            Spacer()
            if item.website != nil {
                Image(systemName: "arrow.up.right.square").foregroundStyle(.secondary)
            }
        }
        if let website = item.website {
            Link(destination: website) { row }
                .foregroundStyle(.primary)
                .accessibilityHint(Text("\(item.name) website"))
        } else {
            row
        }
    }

    @ViewBuilder private var status: some View {
        if item.detected {
            Text(item.version.isEmpty ? String(localized: "Detected") : "\(String(localized: "Detected")) · \(item.version)")
                .font(.caption)
                .foregroundStyle(.statusOK)
        } else {
            Text("Not detected").font(.caption).foregroundStyle(.secondary)
        }
    }
}
