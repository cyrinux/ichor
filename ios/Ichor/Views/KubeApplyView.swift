import SwiftUI
import IchorCore

/// Apply YAML from the clipboard (a snippet from a chat, a runbook, a Gist), like
/// `kubectl apply --server-side`: paste, preview what each object would become in the cluster
/// (a dry run, as a diff), then apply. A field another manager owns is refused with its name,
/// never taken over. Editing the text drops the preview: what is applied is what was previewed.
struct KubeApplyView: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(AppModel.self) private var model
    @State private var manifests = ""
    @State private var namespace = ""
    @State private var preview: LoadState<KubeApplyResult>?
    @State private var applied: LoadState<KubeApplyResult>?
    /// Per object id: what the user folded or unfolded.
    @State private var expanded: [String: Bool] = [:]

    /// Unfolded at first: the first objects that would change or were refused.
    private static let unfoldedAtFirst = 3

    private var busy: Bool { isLoading(preview) || isLoading(applied) }
    private var previewed: KubeApplyResult? {
        if case .loaded(let result, _, _)? = preview { return result }
        return nil
    }
    private var canApply: Bool {
        guard !busy, let previewed, previewed.hasChanges else { return false }
        if case .loaded? = applied { return false }
        return true
    }

    private func isLoading(_ state: LoadState<KubeApplyResult>?) -> Bool {
        if case .loading? = state { return true }
        return false
    }

    var body: some View {
        NavigationStack {
            List {
                Section {
                    TextEditor(text: $manifests)
                        .font(.caption.monospaced())
                        .frame(minHeight: 180)
                        .autocorrectionDisabled()
                        .textInputAutocapitalization(.never)
                        .disabled(busy)
                        .onChange(of: manifests) { _, _ in reset() }
                    HStack {
                        Button {
                            if let text = UIPasteboard.general.string, !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                                manifests = text
                            }
                        } label: {
                            Label("Paste", systemImage: "doc.on.clipboard")
                        }
                        .buttonStyle(.bordered)
                        .disabled(busy)
                        TextField("Namespace if none", text: $namespace)
                            .autocorrectionDisabled()
                            .textInputAutocapitalization(.never)
                            .disabled(busy)
                            .onChange(of: namespace) { _, _ in reset() }
                    }
                } footer: {
                    Text("Paste one or more Kubernetes objects (YAML, separated by ---). Preview shows what each would become in the cluster before anything is applied.")
                }
                if let shown = applied ?? preview {
                    resultSection(shown, isApply: applied != nil)
                }
                Section {
                    EmptyView()
                } footer: {
                    Text("Server-side apply as “ichor”, like kubectl apply --server-side. A field another manager owns (a GitOps controller, an autoscaler) is refused with its name, never taken over.")
                }
            }
            .themedBackground()
            .navigationTitle("Apply YAML")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Close") { dismiss() } }
                ToolbarItemGroup(placement: .confirmationAction) {
                    Button("Preview") { Task { await runPreview() } }
                        .disabled(busy || manifests.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                    Button("Apply") { Task { await runApply() } }
                        .disabled(!canApply)
                }
            }
        }
    }

    @ViewBuilder
    private func resultSection(_ state: LoadState<KubeApplyResult>, isApply: Bool) -> some View {
        switch state {
        case .loading:
            Section { HStack { Spacer(); ProgressView(); Spacer() } }
        case .failed(let message):
            Section {
                Label { Text(verbatim: message).textSelection(.enabled) } icon: { Image(systemName: "exclamationmark.triangle.fill") }
                    .foregroundStyle(.statusBad)
            } header: {
                Text(isApply ? "Not applied" : "Preview failed")
            }
        case .loaded(let result, _, _):
            Section {
                if isApply {
                    Label("\(result.applied) objects applied", systemImage: result.failed == 0 ? "checkmark.circle.fill" : "exclamationmark.circle.fill")
                        .foregroundStyle(result.failed == 0 ? Color.statusOK : Color.statusWarn)
                } else if !result.hasChanges && result.failed == 0 {
                    Label("Nothing would change: the cluster already matches.", systemImage: "checkmark.circle.fill").foregroundStyle(.statusOK)
                }
                if result.failed > 0 {
                    Label("\(result.failed) objects refused", systemImage: "xmark.octagon.fill").foregroundStyle(.statusBad)
                }
                if !result.counts.isEmpty {
                    ChipFlow {
                        ForEach(result.counts, id: \.change) { DiffChangeBadge(change: $0.change, count: $0.count) }
                    }
                }
                ForEach(result.warnings, id: \.self) { warning in
                    Label { Text(verbatim: warning) } icon: { Image(systemName: "exclamationmark.triangle.fill") }
                        .font(.callout)
                        .foregroundStyle(.statusWarn)
                }
            }
            Section {
                ForEach(Array(result.resources.enumerated()), id: \.element.id) { index, resource in
                    DiffResourceRow(resource: resource, expanded: binding(for: resource, at: index))
                }
            }
        }
    }

    private func binding(for resource: KubeDiffResource, at index: Int) -> Binding<Bool> {
        let initial = index < Self.unfoldedAtFirst && (resource.change.isChange || !resource.error.isEmpty)
        return Binding(
            get: { expanded[resource.id] ?? initial },
            set: { expanded[resource.id] = $0 }
        )
    }

    private func reset() {
        preview = nil
        applied = nil
    }

    private func runPreview() async {
        guard let client = model.client else { return }
        let manifests = manifests, namespace = namespace.trimmingCharacters(in: .whitespaces)
        applied = nil
        preview = .loading
        preview = await .from { try await client.applyPreview(namespace: namespace, manifests: manifests) }
    }

    private func runApply() async {
        guard let client = model.client else { return }
        let manifests = manifests, namespace = namespace.trimmingCharacters(in: .whitespaces)
        applied = .loading
        applied = await .from { try await client.apply(namespace: namespace, manifests: manifests) }
    }
}
