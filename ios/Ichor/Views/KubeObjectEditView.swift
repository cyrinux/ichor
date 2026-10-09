import SwiftUI
import IchorCore

/// Edits an object's YAML, then shows what the API server would store (a dry run, as a
/// diff) before anything is saved. The save is an update at the resourceVersion the YAML was
/// read at: a change someone made meanwhile is refused (Conflict), never overwritten.
struct KubeObjectEditView: View {
    let target: KubeEditTarget
    /// Called once saved: the object is read again.
    let onSaved: () -> Void

    @Environment(\.dismiss) private var dismiss
    @Environment(AppModel.self) private var model
    @State private var draft: String
    @State private var reviewing = false
    @State private var confirmingDiscard = false
    /// What refuses the update, once asked; nil while asking, when allowed or unknown (Save
    /// then stays offered and the API server decides).
    @State private var saveDenial: KubeAccess?
    /// The caret in the draft (UTF-16), for the schema help.
    @State private var cursor = 0
    @State private var help: KubeSchemaHelpRequest?

    init(target: KubeEditTarget, onSaved: @escaping () -> Void) {
        self.target = target
        self.onSaved = onSaved
        _draft = State(initialValue: target.yaml)
    }

    private var changed: Bool { draft != target.yaml }

    var body: some View {
        NavigationStack {
            ConfigYamlEditor(text: $draft, error: nil, cursor: $cursor)
                .safeAreaInset(edge: .top, spacing: 0) {
                    VStack(alignment: .leading, spacing: 4) {
                        Label("Nothing changes in the cluster until you review and save.", systemImage: "pencil")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                        if let saveDenial { KubeDeniedLine(text: saveDenial.deniedText) }
                        HStack(spacing: 16) {
                            Button { openHelp(.explain) } label: {
                                Label("Explain field", systemImage: "questionmark.circle")
                            }
                            Button { openHelp(.add) } label: {
                                Label("Add field", systemImage: "plus.circle")
                            }
                        }
                        .font(.footnote)
                        .buttonStyle(.borderless)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal)
                    .padding(.vertical, 6)
                    .background(.bar)
                }
                .navigationTitle(Text(verbatim: target.name))
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        Button("Cancel") {
                            if changed { confirmingDiscard = true } else { dismiss() }
                        }
                    }
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Review changes") { reviewing = true }
                            .disabled(!changed)
                    }
                }
                .navigationDestination(isPresented: $reviewing) {
                    KubeEditReview(target: target, edited: draft, saveDenial: saveDenial) {
                        onSaved()
                        dismiss()
                    }
                }
                .sheet(item: $help) { request in
                    KubeSchemaHelpSheet(resource: target.resource, request: request) { child, listItem in
                        let inserted = insertYamlField(draft, utf16Offset: request.offset, name: child.name, listItem: listItem)
                        cursor = inserted.cursor
                        draft = inserted.text
                    }
                }
                .task(id: target.id) { await loadSaveAccess() }
                .confirmationDialog("Discard your changes?", isPresented: $confirmingDiscard, titleVisibility: .visible) {
                    Button("Discard changes", role: .destructive) { dismiss() }
                    Button("Keep editing", role: .cancel) {}
                }
        }
        .interactiveDismissDisabled(changed)
    }

    /// Opens the schema help for the field at the caret.
    private func openHelp(_ mode: KubeSchemaHelpRequest.Mode) {
        help = KubeSchemaHelpRequest(mode: mode, cursor: yamlCursorAt(draft, utf16Offset: cursor), offset: cursor)
    }

    /// Asks whether the credentials may update this object; a review that cannot be asked
    /// leaves Save offered.
    private func loadSaveAccess() async {
        guard let client = model.client else { return }
        let target = target
        guard let access = try? await client.kubeCan(verb: "update", group: target.resource.group, resource: target.resource.resource,
                                                     namespace: target.namespace, name: target.name),
              !Task.isCancelled else { return }
        saveDenial = access.isDenied ? access : nil
    }
}

/// The dry run's diff of the stored object and the edited one, then Save.
private struct KubeEditReview: View {
    let target: KubeEditTarget
    let edited: String
    /// What refuses the update: Save is then disabled, with the reason.
    let saveDenial: KubeAccess?
    let onSaved: () -> Void

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<KubeEditPreview> = .loading
    @State private var saving = false
    @State private var failure: String?

    var body: some View {
        LoadStateView(state: state, retry: loadPreview) { preview in
            List {
                if let failure { failureSection(failure) }
                if preview.changed {
                    Section {
                        DiffLinesView(lines: preview.lines)
                            .listRowInsets(EdgeInsets(top: 6, leading: 8, bottom: 6, trailing: 8))
                    } header: {
                        let counts = preview.counts
                        HStack(spacing: 10) {
                            Text(verbatim: "+\(counts.added)").foregroundStyle(.statusOK)
                            Text(verbatim: "−\(counts.removed)").foregroundStyle(.statusBad)
                        }
                        .font(.caption.monospacedDigit().weight(.semibold))
                    } footer: {
                        Text("What the API server would store, defaults and webhooks included.")
                    }
                } else {
                    Section {
                        Label("No changes: the API server would store the object as it is.", systemImage: "checkmark.circle")
                            .foregroundStyle(.secondary)
                    }
                }
            }
            .themedBackground()
            .safeAreaInset(edge: .bottom, spacing: 0) {
                if preview.changed { saveBar }
            }
        }
        .navigationTitle(Text("Review changes"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await loadPreview() }
    }

    private var saveBar: some View {
        VStack(alignment: .leading, spacing: 8) {
            if let saveDenial { KubeDeniedLine(text: saveDenial.deniedText) }
            Button {
                Task { await save() }
            } label: {
                HStack {
                    if saving { ProgressView() }
                    Text("Save")
                }
                .frame(maxWidth: .infinity)
            }
            .buttonStyle(.borderedProminent)
            .controlSize(.large)
            .disabled(saving || saveDenial != nil)
        }
        .padding()
        .background(.bar)
    }

    private func failureSection(_ failure: String) -> some View {
        Section {
            Label {
                Text(verbatim: failure).textSelection(.enabled)
            } icon: {
                Image(systemName: "exclamationmark.triangle.fill")
            }
            .foregroundStyle(.statusBad)
            if isKubeEditConflict(failure) {
                Text("Someone changed this object after you opened it. Your edit was not saved: go back, close the editor and open the object again to start from the latest version.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        } header: {
            Text("Not saved")
        }
    }

    private func loadPreview() async {
        guard let client = model.client else { return }
        let target = target
        let edited = edited
        state = .loading
        state = await .from {
            try await client.objectUpdatePreview(target.resource, namespace: target.namespace, name: target.name, edited: edited)
        }
    }

    private func save() async {
        guard let client = model.client, !saving else { return }
        saving = true
        defer { saving = false }
        failure = nil
        do {
            try await client.updateObject(target.resource, namespace: target.namespace, name: target.name, edited: edited)
            onSaved()
        } catch {
            failure = error.localizedDescription
        }
    }
}
