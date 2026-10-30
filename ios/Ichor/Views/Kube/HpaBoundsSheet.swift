import SwiftUI
import IchorCore

/// An HPA's min and max, with the count it runs now between them. Save stays off until the
/// bounds change and while max is below min (min cannot go under 1). Reversible: no typed name.
struct HpaBoundsSheet: View {
    let row: HpaRow
    /// Called once saved, before the sheet closes.
    let onDone: () async -> Void

    @Environment(\.dismiss) private var dismiss
    @Environment(AppModel.self) private var model
    @State private var minReplicas: Int
    @State private var maxReplicas: Int
    @State private var applying = false
    @State private var failure: String?

    // Explicit: the private @State makes the memberwise init private.
    init(row: HpaRow, onDone: @escaping () async -> Void) {
        self.row = row
        self.onDone = onDone
        _minReplicas = State(initialValue: Swift.min(Swift.max(row.min, 1), kubeMaxScaleReplicas))
        _maxReplicas = State(initialValue: Swift.min(Swift.max(row.max, 1), kubeMaxScaleReplicas))
    }

    private var valid: Bool { HpaBounds.isValid(minReplicas, maxReplicas) }
    /// Against what the form started from: an HPA at min 0 (HPAScaleToZero) opens at 1, unsaved.
    private var changed: Bool {
        minReplicas != Swift.min(Swift.max(row.min, 1), kubeMaxScaleReplicas)
            || maxReplicas != Swift.min(Swift.max(row.max, 1), kubeMaxScaleReplicas)
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Stepper(value: $minReplicas, in: 1...kubeMaxScaleReplicas) {
                        LabeledContent("Min replicas") { Text(verbatim: String(minReplicas)).monospacedDigit() }
                    }
                    Text(AutoscaleText.boundsCurrent(String(row.current))).foregroundStyle(.secondary)
                    Stepper(value: $maxReplicas, in: 1...kubeMaxScaleReplicas) {
                        LabeledContent("Max replicas") { Text(verbatim: String(maxReplicas)).monospacedDigit() }
                    }
                } header: {
                    Text(verbatim: row.name)
                } footer: {
                    if !valid {
                        Text("Max must not be below min.").foregroundStyle(.statusBad)
                    }
                }
                .disabled(applying)
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
                        Task { await save() }
                    } label: {
                        HStack {
                            if applying { ProgressView() }
                            Text("Save bounds")
                        }
                    }
                    .disabled(applying || !valid || !changed)
                }
            }
            .navigationTitle(Text(AutoscaleText.boundsTitle(row.name)))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() }.disabled(applying) }
            }
        }
        .interactiveDismissDisabled(applying)
    }

    private func save() async {
        guard let client = model.client, valid else { return }
        applying = true
        failure = nil
        do {
            try await client.setHPABounds(namespace: row.namespace, name: row.name, min: minReplicas, max: maxReplicas)
            await onDone()
            dismiss()
        } catch {
            failure = error.localizedDescription
        }
        applying = false
    }
}
