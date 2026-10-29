import SwiftUI
import IchorCore

/// A node's taints and labels (os:admin on Talos, patch nodes on a cluster without Talos):
/// rows to edit or remove, an add row per section, well-known ones locked unless shown, then
/// Review (the dry-run plan) and Apply. `node`: the Talos address, or the Kubernetes name on a
/// cluster without Talos.
struct NodeMetaEditorView: View {
    let node: String
    let hostname: String

    @Environment(AppModel.self) private var model
    @State private var meta: LoadState<KubeNodeMeta> = .loading
    @State private var draft: NodeMetaDraft?
    @State private var showProtected = false
    @State private var entry: NodeMetaEntry?
    @State private var reviewed: ReviewedNodeMetaPlan?
    @State private var reviewing = false
    @State private var message: String?
    @State private var outcome: String?

    private var kube: Bool { model.activeIsKube }

    var body: some View {
        LoadStateView(state: meta, retry: load) { meta in form(draft ?? NodeMetaDraft(meta)) }
            .navigationTitle(String(localized: "Taints and labels"))
            .navigationBarTitleDisplayMode(.inline)
            .task { await load() }
            // Nodes are cluster-wide.
            .loadsKubeActionAccess(namespace: "")
            .sheet(item: $entry) { entry in
                NodeMetaEntrySheet(entry: entry) { save($0) }
            }
            .sheet(item: $reviewed) { reviewed in
                NodeMetaPlanSheet(
                    plan: reviewed.plan, hostname: hostname,
                    apply: { await apply(reviewed) },
                    reload: { Task { await load() } })
            }
            .alert(outcome ?? "", isPresented: Binding(get: { outcome != nil }, set: { if !$0 { outcome = nil } })) {
                Button("OK", role: .cancel) {}
            }
    }

    private func form(_ draft: NodeMetaDraft) -> some View {
        Form {
            Section {
                Toggle("Show protected", isOn: $showProtected)
            } footer: {
                Text(
                    "Taints and labels set by Kubernetes, Talos or the cloud provider are locked. Show them to change them anyway."
                )
            }
            Section("Taints") {
                if draft.taints.isEmpty { Text("No taints.").foregroundStyle(.secondary) }
                ForEach(draft.taints) { taint in
                    taintRow(taint, editable: NodeMetaDraft.editable(taint, showProtected: showProtected))
                }
                Button {
                    entry = NodeMetaEntry(kind: .taint)
                } label: {
                    Label("Add a taint", systemImage: "plus")
                }
            }
            Section("Labels") {
                if draft.labels.isEmpty { Text("No labels.").foregroundStyle(.secondary) }
                ForEach(draft.labels) { label in
                    labelRow(label, editable: NodeMetaDraft.editable(label, showProtected: showProtected))
                }
                Button {
                    entry = NodeMetaEntry(kind: .label)
                } label: {
                    Label("Add a label", systemImage: "plus")
                }
            }
            Section {
                Button {
                    Task { await review() }
                } label: {
                    if reviewing { ProgressView() } else { Text("Review") }
                }
                .disabled(!draft.hasChanges || reviewing)
                .kubeGated(kube ? .editNodeMeta : nil)
                KubeDeniedNote(kube ? .editNodeMeta : nil)
                if let message { Text(message).font(.footnote).foregroundStyle(.statusBad) }
            }
        }
        .themedBackground()
    }

    private func taintRow(_ taint: NodeTaint, editable: Bool) -> some View {
        NodeMetaRow(text: taint.display, locked: !editable)
            .swipeActions {
                if editable {
                    Button("Remove", role: .destructive) { update { $0.removeTaint(taint) } }
                    Button("Edit") { entry = NodeMetaEntry(editing: taint) }
                }
            }
            .contextMenu {
                if editable {
                    Button("Edit") { entry = NodeMetaEntry(editing: taint) }
                    Button("Remove", role: .destructive) { update { $0.removeTaint(taint) } }
                }
            }
    }

    private func labelRow(_ label: NodeLabel, editable: Bool) -> some View {
        NodeMetaRow(text: label.value.isEmpty ? label.key : "\(label.key)=\(label.value)", locked: !editable)
            .swipeActions {
                if editable {
                    Button("Remove", role: .destructive) { update { $0.removeLabel(label) } }
                    Button("Edit") { entry = NodeMetaEntry(editing: label) }
                }
            }
            .contextMenu {
                if editable {
                    Button("Edit") { entry = NodeMetaEntry(editing: label) }
                    Button("Remove", role: .destructive) { update { $0.removeLabel(label) } }
                }
            }
    }

    /// Changes the draft (made from the read when none yet).
    private func update(_ change: (inout NodeMetaDraft) -> Void) {
        guard var current = draft else { return }
        change(&current)
        draft = current
        message = nil
    }

    /// The add or edit sheet's result.
    private func save(_ entry: NodeMetaEntry) {
        let key = entry.key.trimmingCharacters(in: .whitespaces)
        let value = entry.value.trimmingCharacters(in: .whitespaces)
        switch entry.kind {
        case .taint:
            update {
                $0.setTaint(NodeTaint(key: key, value: value, effect: entry.effect.rawValue), replacing: entry.taint)
            }
        case .label:
            update { $0.setLabel(NodeLabel(key: key, value: value), replacing: entry.label) }
        }
    }

    private func load() async {
        guard let client = model.client else { return }
        let new: LoadState<KubeNodeMeta>
        if kube {
            new = await .from { try await client.kubeNodeMeta(kubeNode: node) }
        } else {
            new = await .from { try await client.nodeMeta(node: node) }
        }
        meta = new
        if case .loaded(let value, _, _) = new { draft = NodeMetaDraft(value) }
        message = nil
    }

    /// The dry run of the draft's edits.
    private func review() async {
        guard let client = model.client, let draft else { return }
        let edits = draft.edits.forcing(showProtected)
        message = nil
        reviewing = true
        defer { reviewing = false }
        do {
            let plan: NodeMetaPlan
            if kube {
                plan = try await client.kubeNodeMetaPlan(kubeNode: node, edits: edits)
            } else {
                plan = try await client.nodeMetaPlan(node: node, edits: edits)
            }
            reviewed = ReviewedNodeMetaPlan(plan: plan, edits: edits)
        } catch {
            message = error.localizedDescription
        }
    }

    /// After the plan sheet's confirmation: the app lock when on, then the apply of the edits
    /// that were planned. Returns why it failed, nil once applied (the sheet then closes).
    private func apply(_ reviewed: ReviewedNodeMetaPlan) async -> String? {
        guard let client = model.client else { return nil }
        let reason =
            reviewed.plan.hasTaintChanges
            ? String(localized: "Change the taints of \(hostname)")
            : String(localized: "Change the labels of \(hostname)?")
        if model.lock.enabled, let failure = await Authenticator.authenticate(reason: reason) {
            return failure
        }
        do {
            if kube {
                try await client.kubeNodeMetaApply(kubeNode: node, edits: reviewed.edits, force: reviewed.edits.force)
            } else {
                try await client.nodeMetaApply(node: node, edits: reviewed.edits, force: reviewed.edits.force)
            }
        } catch {
            return error.localizedDescription
        }
        self.reviewed = nil
        outcome = String(localized: "Taints and labels of \(hostname) updated")
        await load()
        return nil
    }
}

/// A plan and the edits it was made from: Apply sends exactly those.
struct ReviewedNodeMetaPlan: Identifiable {
    let id = UUID()
    let plan: NodeMetaPlan
    let edits: NodeMetaEdits
}

/// A taint ("key=value:Effect") or label ("key=value") row; a lock when it is read-only.
private struct NodeMetaRow: View {
    let text: String
    let locked: Bool

    var body: some View {
        HStack(spacing: 8) {
            Text(verbatim: text).font(.callout.monospaced()).textSelection(.enabled)
            Spacer(minLength: 0)
            if locked {
                Image(systemName: "lock.fill").font(.caption).foregroundStyle(.secondary).accessibilityHidden(true)
            }
        }
        .foregroundStyle(locked ? Color.secondary : Color.primary)
    }
}

/// The add or edit sheet's fields: a taint (key, value, effect) or a label (key, value).
struct NodeMetaEntry: Identifiable {
    enum Kind { case taint, label }

    let id = UUID()
    let kind: Kind
    var key = ""
    var value = ""
    var effect = NodeTaintEffect.noSchedule
    /// The row edited, nil when adding.
    var taint: NodeTaint?
    var label: NodeLabel?

    init(kind: Kind) {
        self.kind = kind
    }

    init(editing taint: NodeTaint) {
        kind = .taint
        key = taint.key
        value = taint.value
        effect = NodeTaintEffect(rawValue: taint.effect) ?? .noSchedule
        self.taint = taint
    }

    init(editing label: NodeLabel) {
        kind = .label
        key = label.key
        value = label.value
        self.label = label
    }

    var isEditing: Bool { taint != nil || label != nil }
}

/// Adds or edits one taint or label of the draft (nothing is sent until Review and Apply).
private struct NodeMetaEntrySheet: View {
    let onSave: (NodeMetaEntry) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var entry: NodeMetaEntry

    init(entry: NodeMetaEntry, onSave: @escaping (NodeMetaEntry) -> Void) {
        self.onSave = onSave
        _entry = State(initialValue: entry)
    }

    private var title: String {
        if entry.isEditing { return String(localized: "Edit") }
        return entry.kind == .taint ? String(localized: "Add a taint") : String(localized: "Add a label")
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("Key", text: $entry.key)
                        .font(.body.monospaced())
                        .autocorrectionDisabled()
                        .textInputAutocapitalization(.never)
                    TextField("Value", text: $entry.value)
                        .font(.body.monospaced())
                        .autocorrectionDisabled()
                        .textInputAutocapitalization(.never)
                    if entry.kind == .taint {
                        Picker("Effect", selection: $entry.effect) {
                            ForEach(NodeTaintEffect.allCases) { effect in
                                Text(verbatim: effect.rawValue).tag(effect)
                            }
                        }
                    }
                }
            }
            .navigationTitle(title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") {
                        onSave(entry)
                        dismiss()
                    }
                    .disabled(entry.key.trimmingCharacters(in: .whitespaces).isEmpty)
                }
            }
        }
    }
}
