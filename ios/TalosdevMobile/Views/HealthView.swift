import SwiftUI
import TalosdevMobileCore

/// Server-side cluster health check (`talosctl health`); needs os:admin.
struct HealthView: View {
    @Environment(AppModel.self) private var model
    @State private var lines: [String] = []
    @State private var running = false
    @State private var finished = false
    @State private var error: String?
    @State private var runID = 0

    var body: some View {
        List {
            if !model.allows(.health) {
                RoleNotice(feature: .health, roles: model.activeSummary?.roles ?? [])
                Text("The overview and etcd screens show node readiness and etcd status with any role.")
                    .font(.footnote).foregroundStyle(.secondary)
            } else {
                Section {
                    HStack {
                        if running {
                            ProgressView()
                            Text("Running server-side checks…")
                        } else if finished && error == nil {
                            StatusPill(label: "Healthy", color: .green)
                            Text("All checks passed")
                        } else if finished {
                            StatusPill(label: "Unhealthy", color: .red)
                        }
                        Spacer()
                        if !running { Button("Re-run") { runID += 1 } }
                    }
                    if let error { Text(error).foregroundStyle(.red) }
                }
                Section {
                    ForEach(Array(lines.enumerated()), id: \.offset) { _, line in
                        Text(line).font(.system(.caption, design: .monospaced))
                    }
                }
            }
        }
        .themedBackground()
        .navigationTitle("Cluster health")
        .task(id: runID) { await run() }
    }

    private func run() async {
        guard model.allows(.health), let client = model.client else { return }
        lines = []
        error = nil
        finished = false
        running = true
        defer { running = false }
        for await event in client.health() {
            switch event {
            case .progress(_, let message): lines.append(message)
            case .done(let failure):
                error = failure
                finished = true
            }
        }
    }
}
