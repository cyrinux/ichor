import SwiftUI
import IchorCore

/// Server-side cluster health check (`talosctl health`); needs os:admin.
struct HealthView: View {
    @Environment(AppModel.self) private var model
    @Environment(AISettings.self) private var ai
    @State private var lines: [String] = []
    @State private var running = false
    @State private var finished = false
    @State private var error: String?
    @State private var runID = 0
    @State private var explanation = HealthExplanation()

    var body: some View {
        List {
            meshSection
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
                            StatusPill(label: String(localized: "Healthy"), color: .green)
                            Text("All checks passed")
                        } else if finished {
                            StatusPill(label: String(localized: "Unhealthy"), color: .red)
                        }
                        Spacer()
                        if !running { Button("Re-run") { runID += 1 } }
                    }
                    if let error {
                        Text(error).foregroundStyle(.statusBad)
                    }
                }
                // Only with the optional AI diagnosis turned on in the settings.
                if finished, ai.enabled, let error {
                    HealthExplainSection(explanation: explanation, lines: lines, failure: error)
                }
                Section {
                    ForEach(Array(lines.enumerated()), id: \.offset) { index, line in
                        Text(line).font(.system(.caption, design: .monospaced))
                            .foregroundStyle(
                                lineColor(healthLineStatus(line, failed: error != nil && index == lines.count - 1)))
                    }
                }
            }
        }
        .themedBackground()
        .navigationTitle("Cluster health")
        .toolbar { ToolbarItem(placement: .primaryAction) { ShareLinkButton(target: .screen(.health)) } }
        .task(id: runID) { await loadMesh(force: runID > 0) }
        .task(id: runID) { await run() }
        .task(id: ai.enabled) { if ai.enabled { await ai.loadProviders() } }
        .onDisappear { explanation.stop() }
    }

    /// The KubeSpan links, which the server's checks do not cover; shown with any role, nothing
    /// without KubeSpan or when they could not be read.
    @ViewBuilder private var meshSection: some View {
        if let mesh = TopologyStore.shared.topology(for: model.topologyKey)?.mesh, mesh.checked {
            Section {
                if mesh.broken > 0 {
                    Text("\(mesh.broken) peer links down").foregroundStyle(.statusBad)
                } else {
                    Text("All \(mesh.links) links up").foregroundStyle(.statusOK)
                }
                ForEach(mesh.pairs, id: \.self) { Text(verbatim: $0).font(.footnote) }
                if mesh.broken > 0, finished, error == nil {
                    Text("Server checks do not cover KubeSpan.").font(.footnote).foregroundStyle(.secondary)
                }
                NavigationLink { KubeSpanView() } label: { Text("Open map") }
            } header: {
                Text(verbatim: "KubeSpan")
            }
        }
    }

    private func loadMesh(force: Bool) async {
        guard model.activeSummary?.isKube != true, let client = model.client else { return }
        await TopologyStore.shared.load(with: client, key: model.topologyKey, force: force)
    }

    private func lineColor(_ status: HealthLineStatus) -> Color {
        switch status {
        case .ok: .statusOK
        case .pending: .secondary
        case .warn: .statusWarn
        case .bad: .statusBad
        case .info: .primary
        }
    }

    private func run() async {
        guard model.allows(.health), let client = model.client else { return }
        explanation.reset()
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
