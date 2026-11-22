import SwiftUI
import IchorCore

/// A node group's desired, minimum and maximum size, prefilled from the group. Next stays off
/// for a size AWS would refuse; then the group's name is typed to confirm. Never retried.
struct NodeGroupScaleSheet: View {
    let row: ManagedNodeGroupRow
    /// Called with the update AWS started, before the sheet closes.
    let onStarted: (NodeGroupUpdate) async -> Void

    @Environment(\.dismiss) private var dismiss
    @Environment(AppModel.self) private var model
    @State private var desired: Int
    @State private var minSize: Int
    @State private var maxSize: Int
    @State private var confirming = false
    @State private var applying = false
    @State private var failure: String?

    // Explicit: the private @State makes the memberwise init private.
    init(row: ManagedNodeGroupRow, onStarted: @escaping (NodeGroupUpdate) async -> Void) {
        self.row = row
        self.onStarted = onStarted
        _desired = State(initialValue: Swift.min(Swift.max(row.desired, 0), nodeGroupMaxSize))
        _minSize = State(initialValue: Swift.min(Swift.max(row.min, 0), nodeGroupMaxSize))
        _maxSize = State(initialValue: Swift.min(Swift.max(row.max, 1), nodeGroupMaxSize))
    }

    private var problem: NodeGroupSizeProblem? { nodeGroupSizeProblem(desired: desired, min: minSize, max: maxSize) }
    private var changed: Bool { desired != row.desired || minSize != row.min || maxSize != row.max }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    stepper("Desired size", $desired, 0...nodeGroupMaxSize)
                    stepper("Minimum", $minSize, 0...nodeGroupMaxSize)
                    stepper("Maximum", $maxSize, 1...nodeGroupMaxSize)
                } header: {
                    Text(verbatim: row.name)
                } footer: {
                    if let problem {
                        Text(Self.message(problem)).foregroundStyle(.statusBad)
                    }
                }
                .disabled(applying)
                Section {
                    Text("Scaling down removes nodes: their pods are evicted. AWS picks which nodes.")
                    Text("An autoscaler that manages this group may change its desired size back.")
                }
                .font(.footnote).foregroundStyle(.secondary)
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
                        confirming = true
                    } label: {
                        HStack {
                            if applying { ProgressView() }
                            Text(
                                applying
                                    ? String(localized: "Changing the size of \(row.name)") : String(localized: "Next"))
                        }
                    }
                    .disabled(applying || problem != nil || !changed)
                }
            }
            .navigationTitle(Text(String(localized: "Size of \(row.name)")))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() }.disabled(applying) }
            }
            .sheet(isPresented: $confirming) { confirmation }
        }
        .interactiveDismissDisabled(applying)
    }

    private var confirmation: some View {
        let summary = String(
            localized: "Desired \(String(desired)), minimum \(String(minSize)), maximum \(String(maxSize)).")
        let warning = String(localized: "Scaling down removes nodes: their pods are evicted. AWS picks which nodes.")
        return HostnameConfirmationSheet(
            title: String(localized: "Change the size of \(row.name)?"),
            message: desired < row.desired ? summary + "\n\n" + warning : summary,
            hostname: row.name,
            actionTitle: String(localized: "Change size")
        ) {
            confirming = false
            Task { await apply() }
        }
    }

    private func stepper(_ label: LocalizedStringKey, _ value: Binding<Int>, _ range: ClosedRange<Int>) -> some View {
        Stepper(value: value, in: range) {
            LabeledContent(label) { Text(verbatim: String(value.wrappedValue)).monospacedDigit() }
        }
    }

    private func apply() async {
        guard let client = model.client, problem == nil else { return }
        applying = true
        failure = nil
        do {
            let update = try await client.scaleNodeGroup(group: row.name, desired: desired, min: minSize, max: maxSize)
            await onStarted(update)
            dismiss()
        } catch {
            failure = error.localizedDescription
        }
        applying = false
    }

    static func message(_ problem: NodeGroupSizeProblem) -> String {
        switch problem {
        case .minNegative: String(localized: "The minimum size must be 0 or more.")
        case .maxTooLow: String(localized: "The maximum size must be at least 1.")
        case .minAboveMax: String(localized: "The minimum size must not be above the maximum.")
        case .desiredOutside: String(localized: "The desired size must be between the minimum and the maximum.")
        }
    }
}
