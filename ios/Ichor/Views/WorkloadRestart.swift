import SwiftUI
import IchorCore

extension View {
    /// Asks before a rollout restart of the workload in `confirm`; `restart` runs on confirmation.
    /// The replicas decide the downtime warning: they are read fresh while the dialog shows.
    func restartConfirmation(_ confirm: Binding<KubeWorkload?>, restart: @escaping (KubeWorkload) -> Void) -> some View {
        modifier(RestartConfirmation(confirm: confirm, restart: restart))
    }
}

private struct RestartConfirmation: ViewModifier {
    @Binding var confirm: KubeWorkload?
    let restart: (KubeWorkload) -> Void

    @Environment(AppModel.self) private var model

    func body(content: Content) -> some View {
        content
            .confirmationDialog(confirm.map { String(localized: "Restart \($0.kind) \($0.name)?") } ?? "",
                                isPresented: $confirm.isPresent(),
                                titleVisibility: .visible,
                                presenting: confirm) { workload in
                Button("Restart", role: .destructive) { restart(workload) }
                Button("Cancel", role: .cancel) {}
            } message: { workload in
                if workload.desired <= 1 {
                    Text("Its pods in \(workload.namespace) are replaced with a rolling update, like kubectl rollout restart.") +
                        Text(verbatim: " ") + Text("With a single pod, the workload is briefly unavailable.")
                } else {
                    Text("Its pods in \(workload.namespace) are replaced with a rolling update, like kubectl rollout restart.")
                }
            }
            // The workload as it is now, unless the dialog was dismissed meanwhile; the one
            // shown when it cannot be read.
            .task(id: confirm?.id) {
                guard let asked = confirm, let client = model.client,
                      let fresh = try? await client.workload(kind: asked.kind, namespace: asked.namespace, name: asked.name),
                      !Task.isCancelled, confirm?.id == asked.id else { return }
                confirm = fresh
            }
    }
}
