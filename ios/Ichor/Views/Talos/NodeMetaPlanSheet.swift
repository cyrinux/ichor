import SwiftUI
import IchorCore

/// The dry run of a node's taint and label edits: the diff, the pods a new NoExecute taint
/// evicts, what blocks it and the warnings, then Apply. A taint change asks for the typed
/// hostname, labels only a plain confirmation; `apply` runs the app lock and the change and
/// returns why it failed (nil: applied, the editor closes this sheet).
struct NodeMetaPlanSheet: View {
    let plan: NodeMetaPlan
    let hostname: String
    let apply: () async -> String?
    /// Re-reads the node (it changed since the editor read it).
    let reload: () -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var confirmingTaints = false
    @State private var confirmingLabels = false
    @State private var applying = false
    @State private var failure: String?

    var body: some View {
        NavigationStack {
            Form {
                if plan.noChange {
                    Section { Text("Nothing changed.").foregroundStyle(.secondary) }
                }
                diffSection(String(localized: "Taints"), plan.taints)
                diffSection(String(localized: "Labels"), plan.labels)
                if !plan.evicts.isEmpty { evictionsSection }
                if !plan.blockers.isEmpty { blockersSection }
                if !plan.warnings.isEmpty {
                    Section("Warnings") {
                        ForEach(plan.warnings, id: \.self) { warning in
                            Label {
                                Text(verbatim: warning)
                            } icon: {
                                Image(systemName: "exclamationmark.triangle.fill")
                            }
                            .foregroundStyle(.statusWarn)
                        }
                    }
                }
                Section {
                    Button(role: plan.hasTaintChanges ? .destructive : nil) {
                        requestApply()
                    } label: {
                        if applying { ProgressView() } else { Text("Apply") }
                    }
                    .disabled(!plan.canApply || applying)
                    if let failure {
                        Text(failure).font(.footnote).foregroundStyle(.statusBad)
                    }
                    // The node changed since the read (the plan says so, or the apply got a
                    // conflict): read it again, the draft starts over from it.
                    if plan.isStale || failure != nil {
                        Button("Reload") { reloadAndClose() }
                    }
                }
            }
            .themedBackground()
            .navigationTitle(String(localized: "Changes to \(hostname)"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
            }
            .sheet(isPresented: $confirmingTaints) {
                HostnameConfirmationSheet(
                    title: String(localized: "Change the taints of \(hostname)"),
                    message: summary,
                    hostname: hostname,
                    actionTitle: String(localized: "Apply")
                ) {
                    confirmingTaints = false
                    run()
                }
            }
            .confirmationDialog(
                String(localized: "Change the labels of \(hostname)?"), isPresented: $confirmingLabels,
                titleVisibility: .visible
            ) {
                Button("Apply") { run() }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text(verbatim: summary)
            }
        }
    }

    @ViewBuilder private func diffSection(_ title: String, _ diff: NodeMetaDiff) -> some View {
        if !diff.isEmpty {
            Section {
                diffRows(String(localized: "Added"), diff.added, systemImage: "plus.circle.fill", color: .statusOK)
                diffRows(
                    String(localized: "Removed"), diff.removed, systemImage: "minus.circle.fill", color: .statusBad)
                diffRows(
                    String(localized: "Changed"), diff.changed, systemImage: "pencil.circle.fill", color: .statusWarn)
            } header: {
                Text(verbatim: title)
            }
        }
    }

    @ViewBuilder private func diffRows(_ kind: String, _ changes: [NodeMetaChange], systemImage: String, color: Color)
        -> some View
    {
        ForEach(changes, id: \.self) { change in
            Label {
                Text(verbatim: change.display).font(.callout.monospaced())
            } icon: {
                Image(systemName: systemImage).foregroundStyle(color)
            }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(Text(verbatim: "\(kind): \(change.display)"))
        }
    }

    private var evictionsSection: some View {
        Section {
            ForEach(plan.evicts) { pod in
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: pod.name).font(.callout.monospaced())
                    Text(verbatim: pod.owner.isEmpty ? pod.namespace : "\(pod.namespace) · \(pod.owner)")
                        .font(.caption).foregroundStyle(.secondary)
                }
                .accessibilityElement(children: .combine)
            }
        } header: {
            Text("These pods are evicted at once:")
        }
    }

    private var blockersSection: some View {
        Section {
            ForEach(plan.blockers, id: \.self) { blocker in
                Label {
                    Text(verbatim: blocker)
                } icon: {
                    Image(systemName: "xmark.octagon.fill")
                }
                .foregroundStyle(.statusBad)
            }
        } header: {
            Text("Cannot apply:")
        }
    }

    /// The change in a few lines, for the confirmation: "+ key=value:Effect", "− …", "~ …".
    private var summary: String {
        let lines = [plan.taints, plan.labels].flatMap { diff in
            diff.added.map { "+ \($0.display)" } + diff.removed.map { "− \($0.display)" }
                + diff.changed.map { "~ \($0.display)" }
        }
        return (lines + plan.warnings).joined(separator: "\n")
    }

    private func requestApply() {
        failure = nil
        if plan.hasTaintChanges {
            confirmingTaints = true
        } else {
            confirmingLabels = true
        }
    }

    private func run() {
        Task {
            applying = true
            failure = await apply()
            applying = false
        }
    }

    private func reloadAndClose() {
        dismiss()
        reload()
    }
}
