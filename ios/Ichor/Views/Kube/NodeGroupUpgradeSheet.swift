import SwiftUI
import IchorCore

/// A node group's upgrade: the control plane's version (when the group is behind it) or the
/// latest node image of its own, and an optional force, off by default; then the group's name
/// is typed to confirm. Every node is replaced. Never retried.
struct NodeGroupUpgradeSheet: View {
    let row: ManagedNodeGroupRow
    let choices: [NodeGroupUpgradeChoice]
    /// Called with the update AWS started, before the sheet closes.
    let onStarted: (NodeGroupUpdate) async -> Void

    @Environment(\.dismiss) private var dismiss
    @Environment(AppModel.self) private var model
    @State private var choice: NodeGroupUpgradeChoice?
    @State private var force = false
    @State private var confirming = false
    @State private var applying = false
    @State private var failure: String?

    // Explicit: the private @State makes the memberwise init private.
    init(
        row: ManagedNodeGroupRow, choices: [NodeGroupUpgradeChoice],
        onStarted: @escaping (NodeGroupUpdate) async -> Void
    ) {
        self.row = row
        self.choices = choices
        self.onStarted = onStarted
        _choice = State(initialValue: choices.first)
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Picker(selection: $choice) {
                        ForEach(choices, id: \.self) { c in
                            Text(label(c)).tag(Optional(c))
                        }
                    } label: {
                        Text(verbatim: row.name)
                    }
                    .pickerStyle(.inline)
                    .labelsHidden()
                } header: {
                    Text(verbatim: row.name)
                }
                .disabled(applying)
                Section {
                    Toggle("Force (ignore pod disruption budgets)", isOn: $force).disabled(applying)
                } footer: {
                    Text(
                        "Every node of the group is replaced, one at a time: its pods are evicted and start again on the new nodes."
                    )
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
                        confirming = true
                    } label: {
                        HStack {
                            if applying { ProgressView() }
                            Text(applying ? String(localized: "Upgrading \(row.name)") : String(localized: "Next"))
                        }
                    }
                    .disabled(applying || choice == nil)
                }
            }
            .navigationTitle(Text(String(localized: "Upgrade \(row.name)")))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() }.disabled(applying) }
            }
            .sheet(isPresented: $confirming) { confirmation }
        }
        .interactiveDismissDisabled(applying)
    }

    private var confirmation: some View {
        let replace = String(
            localized:
                "Every node of the group is replaced, one at a time: its pods are evicted and start again on the new nodes."
        )
        let forced = String(
            localized: "Force is on: pods that a disruption budget protects are deleted if they cannot be evicted.")
        let lines = [choice.map(label) ?? "", replace, force ? forced : ""].filter { !$0.isEmpty }
        return HostnameConfirmationSheet(
            title: String(localized: "Upgrade \(row.name)?"),
            message: lines.joined(separator: "\n\n"),
            hostname: row.name,
            actionTitle: String(localized: "Upgrade")
        ) {
            confirming = false
            Task { await apply() }
        }
    }

    private func label(_ choice: NodeGroupUpgradeChoice) -> String {
        switch choice {
        case .kubernetes(let version): String(localized: "Kubernetes \(version)")
        case .latestImage: String(localized: "Latest node image for \(row.version)")
        }
    }

    private func apply() async {
        guard let client = model.client, let choice else { return }
        applying = true
        failure = nil
        do {
            let update = try await client.nodeGroupUpgrade(group: row.name, version: choice.version, force: force)
            await onStarted(update)
            dismiss()
        } catch {
            failure = error.localizedDescription
        }
        applying = false
    }
}
