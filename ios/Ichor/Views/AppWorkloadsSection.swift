import SwiftUI
import IchorCore

/// The Deployments, StatefulSets and DaemonSets running an app, each with a rollout restart
/// (os:admin): found through its pods' owners in the Kubernetes API when the sheet opens.
struct AppWorkloadsSection: View {
    let app: InventoryApp

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<[KubeWorkload]> = .loading
    /// Those found for app: a restart replaces its pods, so after one the inventory's pod names
    /// no longer lead to their workloads; only their state is fetched again.
    @State private var ids: Set<String>?
    @State private var confirm: KubeWorkload?
    @State private var restarting: Set<String> = []
    @State private var resultMessage: String?

    var body: some View {
        Section("Workloads") {
            switch state {
            case .loading:
                note(Text("Finding its workloads…"))
            case .failed(let message):
                note(Text("Could not list its workloads: \(message)"))
            case .loaded(let workloads, _, _):
                if workloads.isEmpty {
                    note(Text("No Deployment, StatefulSet or DaemonSet runs it."))
                } else {
                    ForEach(workloads) { workload in
                        AppWorkloadLine(workload: workload, restarting: restarting.contains(workload.id)) { confirm = workload }
                    }
                }
            }
        }
        .task(id: app) {
            ids = nil
            state = .loading
            await load()
        }
        .restartConfirmation($confirm) { workload in Task { await restart(workload) } }
        .restartResult($resultMessage)
    }

    private func note(_ text: Text) -> some View {
        text.font(.callout).foregroundStyle(.secondary)
    }

    private func load() async {
        guard let client = model.client else { return }
        let found: LoadState<[KubeWorkload]> = await .from {
            let workloads = try await model.fetch(.workloads, as: KubeWorkloadList.self, with: client).workloads
            if let ids { return workloads.filter { ids.contains($0.id) } }
            guard !app.pods.isEmpty else { return [] }
            let kubePods = try await model.fetch(.pods, as: KubePodList.self, with: client).pods
            return workloadOwners(workloads, pods: app.pods, kubePods: kubePods)
        }
        if ids == nil, case .loaded(let workloads, _, _) = found { ids = Set(workloads.map(\.id)) }
        state = state.refreshed(with: found)
    }

    private func restart(_ workload: KubeWorkload) async {
        guard let client = model.client, !restarting.contains(workload.id) else { return }
        restarting.insert(workload.id)
        defer { restarting.remove(workload.id) }
        do {
            try await client.rolloutRestart(workload)
            resultMessage = String(localized: "\(workload.name) is restarting")
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
