import SwiftUI
import IchorCore

/// Pauses a KEDA ScaledObject at a count (KEDA scales the workload to it and stops), or holds
/// it where it is. Reversible: the sheet is the confirmation.
struct ScaledObjectPauseSheet: View {
    let row: ScaledObjectRow
    /// Called once paused, before the sheet closes.
    let onDone: () async -> Void

    @Environment(\.dismiss) private var dismiss
    @Environment(AppModel.self) private var model
    @State private var count: Int
    @State private var applying = false
    @State private var failure: String?

    // Explicit: the private @State makes the memberwise init private.
    init(row: ScaledObjectRow, onDone: @escaping () async -> Void) {
        self.row = row
        self.onDone = onDone
        _count = State(initialValue: Swift.min(Swift.max(row.pausedReplicas ?? row.current, 0), kubeMaxScaleReplicas))
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Stepper(value: $count, in: 0...kubeMaxScaleReplicas) {
                        LabeledContent("Replicas") { Text(verbatim: String(count)).monospacedDigit() }
                    }
                    .disabled(applying)
                } header: {
                    Text(verbatim: row.name)
                } footer: {
                    Text("KEDA scales the workload to this count and stops scaling it until you resume.")
                }
                Section {
                    if let failure {
                        Label {
                            Text(verbatim: failure).textSelection(.enabled)
                        } icon: {
                            Image(systemName: "exclamationmark.triangle.fill")
                        }
                        .foregroundStyle(.statusBad)
                    }
                    Button {
                        Task { await apply(count) }
                    } label: {
                        HStack {
                            if applying { ProgressView() }
                            Text(AutoscaleText.pauseConfirm(String(count)))
                        }
                    }
                    Button("Hold current count") {
                        Task { await apply(ScaledObjectPause.hold) }
                    }
                } footer: {
                    Text("Hold current count stops KEDA where the workload is now.")
                }
                .disabled(applying)
            }
            .navigationTitle(Text(AutoscaleText.pauseTitle(row.name)))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() }.disabled(applying) }
            }
        }
        .interactiveDismissDisabled(applying)
    }

    private func apply(_ replicas: Int) async {
        guard let client = model.client else { return }
        applying = true
        failure = nil
        do {
            try await client.pauseScaledObject(namespace: row.namespace, name: row.name, replicas: replicas)
            await onDone()
            dismiss()
        } catch {
            failure = error.localizedDescription
        }
        applying = false
    }
}
