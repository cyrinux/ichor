import SwiftUI
import IchorCore

/// Clean up dead pods, like k9s's `z`: the pods of `namespace` ("" for every namespace) whose
/// status is Completed, Pending, CrashLoopBackOff, Error, ImagePullBackOff, Evicted... grouped by
/// status, each checked but a Pending one younger than two minutes, then one plain confirm with
/// the count deletes exactly the checked ones (their owners recreate theirs).
struct SanitizeSheet: View {
    let namespace: String
    /// Called after a sweep deleted something: the pods list loads again.
    let onDeleted: () -> Void

    var body: some View {
        SanitizeContent(namespace: namespace, onDeleted: onDeleted)
            // What the credentials may do there: the delete button reads it.
            .loadsKubeActionAccess(namespace: namespace)
    }
}

private struct SanitizeContent: View {
    let namespace: String
    let onDeleted: () -> Void

    @Environment(\.dismiss) private var dismiss
    @Environment(AppModel.self) private var model
    @State private var preview: LoadState<KubeSanitizePreview> = .loading
    @State private var selected: Set<String> = []
    @State private var confirming = false
    @State private var deleting = false
    @State private var failure: String?
    @State private var failed: [SanitizeFailure] = []
    /// How many the last sweep deleted, shown with the ones it could not.
    @State private var deletedCount = 0

    var body: some View {
        NavigationStack {
            LoadStateView(state: preview, retry: loadPreview) { preview in
                content(preview)
            }
            .navigationTitle(Text("Clean up dead pods"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() }.disabled(deleting) }
            }
            .task { await loadPreview() }
        }
        .interactiveDismissDisabled(deleting)
    }

    @ViewBuilder
    private func content(_ preview: KubeSanitizePreview) -> some View {
        if preview.pods.isEmpty {
            ContentUnavailableView {
                Label("Nothing to clean up", systemImage: "checkmark.circle")
            } description: {
                VStack(spacing: 8) {
                    subtitle
                    if preview.truncated { truncatedText }
                }
            }
            .themedBackground()
        } else {
            form(preview)
        }
    }

    private func form(_ preview: KubeSanitizePreview) -> some View {
        let count = preview.pods.filter { selected.contains($0.id) }.count
        return Form {
            Section {
                subtitle.font(.footnote).foregroundStyle(.secondary)
            }
            if preview.truncated {
                Section {
                    Label {
                        truncatedText
                    } icon: {
                        Image(systemName: "exclamationmark.triangle")
                    }
                    .foregroundStyle(.statusWarn)
                }
            }
            ForEach(groupSanitizePods(preview.pods)) { group in
                Section {
                    ForEach(group.pods) { pod in
                        Toggle(isOn: checked(pod)) {
                            SanitizeRow(pod: pod, showNamespace: namespace.isEmpty)
                        }
                        .disabled(deleting)
                    }
                } header: {
                    HStack {
                        Text(verbatim: group.status)
                        Spacer()
                        Text(verbatim: "\(group.pods.count)").monospacedDigit()
                    }
                }
            }
            if !failed.isEmpty {
                Section {
                    ForEach(failed, id: \.self) { failure in
                        Text("Could not delete \(failure.name): \(failure.error)")
                            .font(.caption)
                            .foregroundStyle(.statusBad)
                            .textSelection(.enabled)
                    }
                } header: {
                    Text("\(deletedCount) pods deleted")
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
                KubeDeniedNote(.sanitizePods)
                Button(role: .destructive) {
                    confirming = true
                } label: {
                    HStack {
                        if deleting { ProgressView() }
                        Text("Delete \(count) pods")
                    }
                }
                .disabled(deleting || count == 0)
                .kubeGated(.sanitizePods)
            }
        }
        .confirmationDialog(String(localized: "Delete \(count) pods?"), isPresented: $confirming, titleVisibility: .visible) {
            Button("Delete", role: .destructive) { Task { await sweep(preview) } }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("Pods a controller owns come back as new ones; the others are gone for good.")
        }
    }

    /// What the sweep picks, and where.
    private var subtitle: Text {
        namespace.isEmpty
            ? Text("Done or stuck pods in every namespace, as k9s sanitize picks them")
            : Text("Done or stuck pods in \(namespace), as k9s sanitize picks them")
    }

    private var truncatedText: Text {
        Text("More pods than one page: only the first page was checked. Pick a namespace to see the rest.")
    }

    private func checked(_ pod: SanitizePod) -> Binding<Bool> {
        Binding(
            get: { selected.contains(pod.id) },
            set: { on in selected = on ? selected.union([pod.id]) : selected.subtracting([pod.id]) }
        )
    }

    private func loadPreview() async {
        guard let client = model.client else { return }
        let namespace = namespace
        preview = .loading
        let fetched: LoadState<KubeSanitizePreview> = await .from { try await client.sanitizePreview(namespace: namespace) }
        if case .loaded(let loaded, _, _) = fetched { selected = sanitizeDefaultSelection(loaded.pods) }
        preview = fetched
    }

    /// Deletes the checked pods; closes when each went, else lists the ones that did not and
    /// reads the preview again.
    private func sweep(_ preview: KubeSanitizePreview) async {
        guard let client = model.client, !deleting else { return }
        let names = sanitizeNames(preview.pods, selected: selected)
        guard !names.isEmpty else { return }
        deleting = true
        defer { deleting = false }
        failure = nil
        failed = []
        do {
            let result = try await client.sanitize(namespace: namespace, names: names)
            if result.deleted > 0 { onDeleted() }
            if result.failed.isEmpty {
                dismiss()
                return
            }
            failed = result.failed
            deletedCount = result.deleted
            await loadPreview()
        } catch {
            failure = error.localizedDescription
        }
    }
}

/// A pod of the sweep: its name, how old it is, and who recreates it.
private struct SanitizeRow: View {
    let pod: SanitizePod
    let showNamespace: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(verbatim: showNamespace ? pod.id : pod.name)
                .font(.callout.monospaced())
                .lineLimit(1)
                .truncationMode(.middle)
            Text("Age \(localizedDuration(pod.ageSec))")
                .font(.caption)
                .foregroundStyle(.secondary)
                .monospacedDigit()
            if pod.hasOwner {
                Text("recreated by \(pod.ownerKind) \(pod.ownerName)")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            if !sanitizeCheckedByDefault(pod) {
                Text("Pending for under 2 minutes: may still be scheduling, left unchecked")
                    .font(.caption)
                    .foregroundStyle(.statusWarn)
            }
        }
        .accessibilityElement(children: .combine)
    }
}
