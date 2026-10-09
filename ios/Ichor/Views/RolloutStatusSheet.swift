import SwiftUI
import IchorCore

/// The live rollout of a workload just restarted, like `kubectl rollout status`: the Go core
/// reads it again each time the workload or one of its pods changes, until every pod runs the
/// new template and is ready. Closing it only stops following; the rollout goes on in the
/// cluster. `ended` runs once it is done, to refresh the list behind.
struct RolloutStatusSheet: View {
    let workload: KubeWorkload
    let ended: () async -> Void

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @Environment(\.scenePhase) private var scenePhase
    @State private var status: KubeRolloutStatus?
    @State private var error: String?
    /// Whether `ended` ran: once, when the rollout was first seen done.
    @State private var reported = false

    var body: some View {
        NavigationStack {
            List {
                Section {
                    VStack(alignment: .leading, spacing: 6) {
                        Text(verbatim: "\(workload.kind) · \(workload.namespace)")
                            .font(.caption.monospaced())
                            .foregroundStyle(.secondary)
                        Text(stateLabel).font(.headline).foregroundStyle(stateColor)
                        progress
                    }
                    if let error {
                        Text(error).font(.caption).foregroundStyle(.red)
                    }
                } footer: {
                    if status?.done != true {
                        Text("The rollout goes on in the cluster if you close this.")
                    }
                }
                if let pods = status?.pods {
                    Section("Pods") {
                        if pods.isEmpty {
                            Text("No pods yet.").foregroundStyle(.secondary)
                        }
                        ForEach(pods) { RolloutPodRow(pod: $0) }
                    }
                }
            }
            .navigationTitle(Text("\(workload.name) is restarting"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Close") { dismiss() }
                }
            }
        }
        .presentationDetents([.medium, .large])
        .task(id: scenePhase == .active) { await follow() }
    }

    @ViewBuilder private var progress: some View {
        if let status {
            let w = status.workload
            let newStarting = status.pods.filter { $0.updated && !$0.healthy }.count
            let fraction = status.done ? 1 : Double(min(status.newReady, w.desired)) / Double(max(w.desired, 1))
            RolloutProgressBar(desired: w.desired, newReady: status.newReady, newStarting: newStarting,
                               done: status.done && !status.manual, failed: status.failed)
            HStack {
                Text("Updated \(w.updated)/\(w.desired) · ready \(w.ready)/\(w.desired) · available \(w.available)/\(w.desired)")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Spacer(minLength: 8)
                if !status.manual {
                    Text(fraction, format: .percent.precision(.fractionLength(0)))
                        .font(.callout.weight(.semibold))
                        .contentTransition(.numericText())
                        .animation(.default, value: fraction)
                }
            }
            .monospacedDigit()
        } else {
            ProgressView().frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    private var stateLabel: LocalizedStringKey {
        guard let status else { return "Starting the rollout…" }
        if status.manual { return "Pods are only replaced when deleted (OnDelete): delete them to restart them." }
        if status.done { return "Rollout complete" }
        if status.failed { return "Stalled: the progress deadline was exceeded" }
        return "rolling out"
    }

    private var stateColor: Color {
        guard let status else { return .secondary }
        if status.manual { return .orange }
        return status.done ? .green : status.failed ? .red : .orange
    }

    /// Follows the rollout live while the app is active; a watch that ends (the network) is
    /// followed again after 2 s. Cancelled when the sheet closes.
    private func follow() async {
        guard scenePhase == .active else { return }
        while !Task.isCancelled {
            guard let client = model.client else { return }
            for await event in client.rolloutWatch(workload) {
                switch event {
                case .update(let latest):
                    status = latest
                    error = nil
                    if latest.done, !reported {
                        reported = true
                        await ended()
                    }
                case .done(let message):
                    if let message { error = message }
                }
            }
            try? await Task.sleep(for: .seconds(2))
        }
    }
}

/// Name, status, ready containers and restarts, tagged new or old.
private struct RolloutPodRow: View {
    let pod: KubeRolloutPod

    var body: some View {
        HStack(spacing: 8) {
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: pod.name)
                    .font(.callout.monospaced())
                    .lineLimit(1)
                    .truncationMode(.middle)
                HStack(spacing: 6) {
                    Text(verbatim: pod.status).foregroundStyle(statusColor)
                    Text("\(pod.ready)/\(pod.containers) ready").foregroundStyle(.secondary)
                    if pod.restarts > 0 {
                        Text("\(pod.restarts) restarts").foregroundStyle(.secondary)
                    }
                }
                .font(.caption)
                .monospacedDigit()
            }
            Spacer(minLength: 8)
            Text(pod.updated ? LocalizedStringKey("new") : LocalizedStringKey("old"))
                .font(.caption2.weight(.semibold))
                .padding(.horizontal, 8)
                .padding(.vertical, 2)
                .foregroundStyle(pod.updated ? .green : .secondary)
                .background((pod.updated ? Color.green : Color.secondary).opacity(0.15), in: Capsule())
        }
    }

    private var statusColor: Color {
        if pod.healthy { return .green }
        if ["Error", "BackOff", "Failed", "OOM", "Err"].contains(where: { pod.status.contains($0) }) { return .red }
        return pod.status == "Terminating" ? .secondary : .orange
    }
}
