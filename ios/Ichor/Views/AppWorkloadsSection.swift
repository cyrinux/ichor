import SwiftUI
import IchorCore

/// The Deployments, StatefulSets and DaemonSets running an app, each with a rollout restart
/// (os:admin): found through its pods' owners in the Kubernetes API when the sheet opens; only
/// those pods and workloads are read, never a cluster-wide list.
struct AppWorkloadsSection: View {
    let app: InventoryApp

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<[KubeWorkload]> = .loading
    /// Those found for app: a restart replaces its pods, so after one the inventory's pod names
    /// no longer lead to their workloads; only their state is fetched again.
    @State private var found: [KubeWorkload]?
    @State private var confirm: KubeWorkload?
    @State private var restarting: Set<String> = []
    @State private var resultMessage: String?
    /// Just restarted: its rollout is shown live until the sheet is closed.
    @State private var following: KubeWorkload?

    var body: some View {
        Section("Workloads") {
            switch state {
            case .loading:
                Text("Finding its workloads…").note()
            case .failed(let message):
                Text("Could not list its workloads: \(message)").note()
            case .loaded(let workloads, _, _):
                if workloads.isEmpty {
                    Text("No Deployment, StatefulSet or DaemonSet runs it.").note()
                } else {
                    ForEach(workloads) { workload in
                        AppWorkloadLine(workload: workload, restarting: restarting.contains(workload.id)) { confirm = workload }
                    }
                }
            }
        }
        .task(id: app) {
            found = nil
            state = .loading
            await load()
        }
        .restartConfirmation($confirm) { workload in Task { await restart(workload) } }
        .sheet(item: $following) { workload in RolloutStatusSheet(workload: workload) { await load() } }
        .messageAlert($resultMessage)
    }

    private func load() async {
        guard let client = model.client else { return }
        let known = found
        let pods = app.routePods
        let result: LoadState<[KubeWorkload]> = await .from {
            // One deleted since drops out.
            if let known { return try await client.workloadsNamed(known.map(\.ref)) }
            guard !pods.isEmpty else { return [] }
            return try await client.appWorkloads(pods: pods)
        }
        if found == nil, case .loaded(let workloads, _, _) = result { found = workloads }
        state = state.refreshed(with: result)
    }

    private func restart(_ workload: KubeWorkload) async {
        guard let client = model.client, !restarting.contains(workload.id) else { return }
        restarting.insert(workload.id)
        defer { restarting.remove(workload.id) }
        do {
            try await client.rolloutRestart(workload)
            following = workload
            // Show the rollout starting: the controller already bumped the generation.
            await load()
        } catch {
            resultMessage = String(localized: "Could not restart \(workload.name): \(error.localizedDescription)")
        }
    }
}

/// Name, "Deployment · 2/3 ready", and the restart button (a spinner while it runs).
private struct AppWorkloadLine: View {
    let workload: KubeWorkload
    let restarting: Bool
    let onRestart: () -> Void

    var body: some View {
        HStack(spacing: 10) {
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: workload.name)
                    .font(.callout.monospaced())
                    .lineLimit(1)
                    .truncationMode(.middle)
                Text(verbatim: "\(workload.kind) · \(workload.ready)/\(workload.desired)")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .monospacedDigit()
            }
            Spacer(minLength: 8)
            if restarting {
                ProgressView()
            } else {
                Button("Restart", systemImage: "arrow.clockwise", action: onRestart)
                    .buttonStyle(.bordered)
                    .disabled(!workload.canRestart)
            }
        }
    }
}
