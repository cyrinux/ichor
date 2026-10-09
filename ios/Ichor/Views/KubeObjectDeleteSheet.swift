import SwiftUI
import IchorCore

/// Confirms deleting an object of the browser: what the deletion would touch (protection,
/// finalizers, the objects it owns, read first), what happens to what it owns, and, for a
/// cluster-scoped or protected object, its name typed as for the risky node actions. A
/// protected object is deleted only through "Delete anyway" (force). The delete carries the
/// version the preview read: an object changed meanwhile is not deleted.
struct KubeObjectDeleteSheet: View {
    let resource: KubeAPIResource
    /// "" for a cluster-scoped object.
    let namespace: String
    let name: String
    /// What refuses the delete (the credentials' role): the button is then disabled, with it.
    let denial: KubeAccess?
    /// Called once deleted, before the sheet closes.
    let onDeleted: () -> Void

    @Environment(\.dismiss) private var dismiss
    @Environment(AppModel.self) private var model
    @State private var preview: LoadState<KubeDeletePreview> = .loading
    @State private var propagation = KubeDeletePropagation.background
    @State private var typed = ""
    @State private var deleting = false
    @State private var failure: String?

    // Explicit: the private @State makes the memberwise init private.
    init(resource: KubeAPIResource, namespace: String, name: String, denial: KubeAccess?, onDeleted: @escaping () -> Void) {
        self.resource = resource
        self.namespace = namespace
        self.name = name
        self.denial = denial
        self.onDeleted = onDeleted
    }

    var body: some View {
        NavigationStack {
            LoadStateView(state: preview, retry: loadPreview) { preview in
                form(preview)
            }
            .navigationTitle(Text("Delete \(name)?"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() }.disabled(deleting) }
            }
            .task { await loadPreview() }
        }
        .interactiveDismissDisabled(deleting)
    }

    private func form(_ preview: KubeDeletePreview) -> some View {
        Form {
            if preview.isProtected {
                Section {
                    Label {
                        Text("Protected: \(preview.reason)")
                    } icon: {
                        Image(systemName: "exclamationmark.shield.fill")
                    }
                    .foregroundStyle(.statusBad)
                }
            }
            if preview.deleting {
                Section {
                    Text("Its deletion is already pending: the finalizers below hold it.")
                        .foregroundStyle(.statusWarn)
                }
            }
            if !preview.finalizers.isEmpty {
                Section {
                    ForEach(preview.finalizers, id: \.self) { Text(verbatim: $0).font(.callout.monospaced()) }
                } header: {
                    Text("Finalizers")
                } footer: {
                    Text("The object stays until their controllers remove them.")
                }
            }
            if !preview.dependents.isEmpty {
                Section {
                    ForEach(preview.dependents, id: \.self) { dependent in
                        HStack(spacing: 6) {
                            Text(verbatim: dependent.kind).foregroundStyle(.secondary)
                            Text(verbatim: dependent.name).font(.callout.monospaced()).lineLimit(1).truncationMode(.middle)
                        }
                    }
                    if preview.moreDependents > 0 {
                        Text("and \(preview.moreDependents) more").foregroundStyle(.secondary)
                    }
                } header: {
                    Text("Objects it owns: \(preview.dependents.count + preview.moreDependents)")
                }
            }
            Section {
                Picker(selection: $propagation) {
                    ForEach(KubeDeletePropagation.allCases) { option in
                        propagationTitle(option).tag(option)
                    }
                } label: {
                    Text("What it owns")
                }
                .pickerStyle(.inline)
                .labelsHidden()
            } header: {
                Text("What it owns")
            } footer: {
                propagationDescription(propagation)
            }
            if preview.needsTypedName {
                Section("Type \(name) to confirm") {
                    TextField("Name", text: $typed, prompt: Text(verbatim: name))
                        .font(.body.monospaced())
                        .autocorrectionDisabled()
                        .textInputAutocapitalization(.never)
                }
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
                if let denial { KubeDeniedLine(text: denial.deniedText) }
                Button(role: .destructive) {
                    Task { await delete(preview) }
                } label: {
                    HStack {
                        if deleting { ProgressView() }
                        if preview.isProtected { Text("Delete anyway") } else { Text("Delete") }
                    }
                }
                .disabled(deleting || denial != nil || (preview.needsTypedName && !typedConfirmationMatches(typed, token: name)))
            }
        }
    }

    @ViewBuilder private func propagationTitle(_ option: KubeDeletePropagation) -> some View {
        switch option {
        case .background: Text("Delete in the background")
        case .foreground: Text("Delete it first")
        case .orphan: Text("Keep it")
        }
    }

    @ViewBuilder private func propagationDescription(_ option: KubeDeletePropagation) -> some View {
        switch option {
        case .background: Text("The object goes now; what it owns is deleted afterwards.")
        case .foreground: Text("What it owns is deleted first; the object waits for it.")
        case .orphan: Text("What it owns stays, without an owner.")
        }
    }

    private func loadPreview() async {
        guard let client = model.client else { return }
        let resource = resource
        let namespace = namespace
        let name = name
        preview = .loading
        preview = await .from {
            try await client.objectDeletePreview(resource, namespace: namespace, name: name)
        }
    }

    private func delete(_ preview: KubeDeletePreview) async {
        guard let client = model.client, !deleting else { return }
        deleting = true
        defer { deleting = false }
        failure = nil
        do {
            try await client.deleteObject(resource, namespace: namespace, name: name, propagation: propagation,
                                          resourceVersion: preview.resourceVersion, force: preview.isProtected)
            onDeleted()
            dismiss()
        } catch {
            failure = error.localizedDescription
        }
    }
}
