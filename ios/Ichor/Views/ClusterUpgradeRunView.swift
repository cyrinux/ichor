import SwiftUI
import IchorCore

/// The roll as it goes: where it stands, every node in order (the one being upgraded with its
/// phase), Pause / Resume / Abort, then how it ended.
struct ClusterUpgradeRunView: View {
    let job: ClusterUpgradeJob
    let onClose: () -> Void

    @State private var confirmAbort = false

    var body: some View {
        List {
            Section {
                if let outcome = job.outcome {
                    if let error = outcome {
                        Label { Text(verbatim: error) } icon: { Image(systemName: "xmark.octagon.fill") }.foregroundStyle(.statusBad)
                    } else {
                        Label("Every node runs \(job.version ?? "").", systemImage: "checkmark.seal.fill").foregroundStyle(.statusOK)
                    }
                } else if let p = job.progress, p.total > 0 {
                    VStack(alignment: .leading, spacing: 4) {
                        Text("Upgrading \(min(p.index + 1, p.total))/\(p.total): \(p.name), \(phaseText(p))").font(.headline)
                        // Why it paused (a failed gate), or the gate's own progress.
                        if !p.message.isEmpty {
                            Text(verbatim: p.message).font(.footnote).foregroundStyle(job.paused ? Color.statusWarn : Color.secondary)
                        }
                    }
                } else {
                    Label("Starting…", systemImage: "hourglass")
                }
            } header: {
                Text("Upgrading the cluster to \(job.version ?? "")")
            }
            Section("Nodes") {
                ForEach(job.nodes) { node in row(node) }
            }
            Section {
                if job.isActive {
                    if job.aborting {
                        Text("Stopping before the next node…").foregroundStyle(.secondary)
                    } else {
                        if job.paused {
                            Button("Resume") { job.resume() }
                        } else {
                            Button("Pause") { job.pause() }
                        }
                        Button("Abort", role: .destructive) { confirmAbort = true }
                    }
                } else {
                    Button("Close", action: onClose)
                }
            } footer: {
                if job.isActive {
                    Text("Keep Ichor open until the end: if the app stops, start again and it continues with the nodes left.")
                }
            }
        }
        .themedBackground()
        .confirmationDialog("Abort the cluster upgrade?", isPresented: $confirmAbort, titleVisibility: .visible) {
            Button("Abort", role: .destructive) { job.abort() }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("It stops before the next node: a node being upgraded finishes first. The nodes upgraded stay upgraded; start again later to continue with the others.")
        }
    }

    private func row(_ node: ClusterUpgradeNode) -> some View {
        let current = job.isActive && job.progress?.node == node.node
        return HStack(spacing: 12) {
            switch node.nodeState {
            case .running: ProgressView()
            case .done: Image(systemName: "checkmark.circle.fill").foregroundStyle(.statusOK)
            case .failed: Image(systemName: "xmark.octagon.fill").foregroundStyle(.statusBad)
            case .pending: Image(systemName: "circle").foregroundStyle(.secondary)
            }
            VStack(alignment: .leading) {
                Text(verbatim: node.name)
                if current, let p = job.progress, p.rollPhase == .node {
                    Text(verbatim: phaseText(p)).font(.caption).foregroundStyle(.secondary)
                }
            }
        }
        .accessibilityElement(children: .combine)
    }

    /// What the roll does now: the node's own upgrade phase, the gate, or the pause.
    private func phaseText(_ p: ClusterUpgradeProgress) -> String {
        switch p.rollPhase {
        case .gate: String(localized: "checking the cluster's health")
        case .paused: String(localized: "paused")
        case .done: String(localized: "Done")
        case .node: p.nodePhase.isEmpty ? String(localized: "Requested") : p.nodePhase
        }
    }
}
