import SwiftUI
import IchorCore

extension View {
    /// Asks before a rollout restart of the workload in `confirm`; `restart` runs on confirmation.
    func restartConfirmation(_ confirm: Binding<KubeWorkload?>, restart: @escaping (KubeWorkload) -> Void) -> some View {
        confirmationDialog(confirm.wrappedValue.map { String(localized: "Restart \($0.kind) \($0.name)?") } ?? "",
                           isPresented: confirm.isPresent(),
                           titleVisibility: .visible,
                           presenting: confirm.wrappedValue) { workload in
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
    }
}
