import SwiftUI
import IchorCore

/// Sets the replicas a source file declares (a workload's spec.replicas, or a kustomization's
/// `replicas:` entry) from the file's value with a stepper. The file is rewritten by the Go
/// core (every other byte kept) and checked here; the editor then applies it at the branch
/// and opens the review.
struct QuickEditReplicasSheet: View {
    let targets: [QuickEditReplicaTarget]
    /// The file as shown, to check the edit changes something.
    let content: String
    /// The edit chosen; the caller closes the sheet and opens the editor.
    let onApply: (QuickEdit) -> Void

    // Explicit: the private @State properties make the memberwise init private.
    init(targets: [QuickEditReplicaTarget], content: String, onApply: @escaping (QuickEdit) -> Void) {
        self.targets = targets
        self.content = content
        self.onApply = onApply
        let first = targets.first
        _place = State(initialValue: first?.place)
        _replicas = State(initialValue: first?.current ?? 1)
    }

    @Environment(\.dismiss) private var dismiss
    @State private var place: QuickEditReplicaPlace?
    @State private var replicas: Int
    @State private var checking = false
    @State private var message: String?

    private var target: QuickEditReplicaTarget? { targets.first { $0.place == place } ?? targets.first }

    var body: some View {
        NavigationStack {
            Group {
                if let target { form(target) }
            }
            .navigationTitle(Text("Set replicas"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() }.disabled(checking) }
            }
        }
        .interactiveDismissDisabled(checking)
        .onChange(of: place) { _, _ in
            replicas = target?.current ?? 1
            message = nil
        }
        .onChange(of: replicas) { _, _ in message = nil }
    }

    private func form(_ target: QuickEditReplicaTarget) -> some View {
        Form {
            Section {
                if targets.count > 1 {
                    Picker("Workload", selection: $place) {
                        ForEach(targets) { option in
                            Text(verbatim: option.label).tag(Optional(option.place))
                        }
                    }
                }
                Stepper(value: $replicas, in: 0...kubeMaxScaleReplicas) {
                    LabeledContent("Replicas") {
                        Text(verbatim: replicas == target.current ? "\(replicas)" : "\(target.current) → \(replicas)")
                            .monospacedDigit()
                    }
                }
            } header: {
                Text(verbatim: target.label)
            }
            Section {
                if let message {
                    Label {
                        Text(verbatim: message).textSelection(.enabled)
                    } icon: {
                        Image(systemName: "info.circle")
                    }
                    .foregroundStyle(.secondary)
                }
                Button {
                    Task { await review(target) }
                } label: {
                    HStack {
                        if checking { ProgressView() }
                        Text("Review change")
                    }
                }
                .disabled(checking || replicas == target.current)
            }
        }
        .themedBackground()
        .disabled(checking)
    }

    /// Checks the edit changes the file, then hands it on.
    private func review(_ target: QuickEditReplicaTarget) async {
        let edit = target.edit(replicas: replicas)
        checking = true
        defer { checking = false }
        message = nil
        do {
            let result = try await TalosClient.quickEdit(content, edit)
            if result.changed {
                onApply(edit)
            } else {
                message = String(localized: "This value is already set.")
            }
        } catch {
            message = error.localizedDescription
        }
    }
}
