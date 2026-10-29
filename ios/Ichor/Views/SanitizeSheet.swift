import SwiftUI
import IchorCore

/// Clean up dead pods, like k9s's `z`: the pods of `namespace` ("" for every namespace) whose
/// status is Completed, Pending, CrashLoopBackOff, Error, ImagePullBackOff, Evicted... grouped by
/// status, each checked but a Pending one younger than two minutes, then one plain confirm with
/// the count starts deleting exactly the checked ones in the background (PodSweepJob, followed
/// by its Live Activity) and closes the sheet. Opened while that sweep runs, it shows how far
/// it is with Stop; once it ended, how many went and the pods it could not delete.
struct SanitizeSheet: View {
    let namespace: String

    var body: some View {
        SanitizeContent(namespace: namespace)
            // What the credentials may do there: the delete button reads it.
            .loadsKubeActionAccess(namespace: namespace)
    }
}

private struct SanitizeContent: View {
    let namespace: String

    @Environment(\.dismiss) private var dismiss
    @Environment(AppModel.self) private var model
    @State private var preview: LoadState<KubeSanitizePreview> = .loading
    @State private var selected: Set<String> = []
    @State private var confirming = false
    @State private var failure: String?

    private var job: PodSweepJob { PodSweepJob.shared }
    /// This namespace's sweep in the cluster on screen, running or the last one.
    private var sweep: PodSweepState? { job.sweep(context: model.activeContext, namespace: namespace) }

    var body: some View {
        NavigationStack {
            Group {
                if let sweep, sweep.running {
                    progress(sweep)
                } else {
                    LoadStateView(state: preview, retry: loadPreview) { preview in
                        content(preview)
                    }
                }
            }
            .navigationTitle(Text("Clean up dead pods"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
            }
            .task { await loadPreview() }
            // What is still dead once the sweep ended.
            .onChange(of: sweep?.running) { was, now in
                if was == true, now == false { Task { await loadPreview() } }
            }
        }
    }

    /// The running sweep: "12 of 140 pods", a bar, and Stop.
    private func progress(_ sweep: PodSweepState) -> some View {
        Form {
            Section {
                VStack(alignment: .leading, spacing: 8) {
                    Text(verbatim: PodSweepJob.step(sweep))
                    ProgressView(value: sweep.fraction)
                }
                .accessibilityElement(children: .combine)
            } footer: {
                Text("Keeps going while the app is open or shortly after.")
            }
            Section {
                Button("Stop", role: .destructive) { job.stop() }
                    .disabled(sweep.stopping)
            }
        }
    }

    @ViewBuilder
    private func content(_ preview: KubeSanitizePreview) -> some View {
        if preview.pods.isEmpty && sweep == nil {
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
            if let sweep { outcome(sweep) }
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
                    }
                } header: {
                    HStack {
                        Text(verbatim: group.status)
                        Spacer()
                        Text(verbatim: "\(group.pods.count)").monospacedDigit()
                    }
                }
            }
            if !preview.pods.isEmpty {
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
                        Text("Delete \(count) pods")
                    }
                    .disabled(count == 0)
                    .kubeGated(.sanitizePods)
                }
            }
        }
        .confirmationDialog(
            String(localized: "Delete \(count) pods?"), isPresented: $confirming, titleVisibility: .visible
        ) {
            Button("Delete", role: .destructive) { start(preview) }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("Pods a controller owns come back as new ones; the others are gone for good.")
        }
    }

    /// How the last sweep went: its error, or how many went and each pod it could not delete.
    @ViewBuilder
    private func outcome(_ sweep: PodSweepState) -> some View {
        Section {
            if let error = sweep.error {
                Text(verbatim: error)
                    .font(.caption)
                    .foregroundStyle(.statusBad)
                    .textSelection(.enabled)
            } else {
                Text(verbatim: PodSweepJob.result(sweep))
            }
            ForEach(sweep.failures, id: \.self) { failure in
                Text("Could not delete \(failure.name): \(failure.error)")
                    .font(.caption)
                    .foregroundStyle(.statusBad)
                    .textSelection(.enabled)
            }
        } header: {
            Text(verbatim: PodSweepJob.title(sweep))
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
        let fetched: LoadState<KubeSanitizePreview> = await .from {
            try await client.sanitizePreview(namespace: namespace)
        }
        if case .loaded(let loaded, _, _) = fetched { selected = sanitizeDefaultSelection(loaded.pods) }
        preview = fetched
    }

    /// Starts deleting the checked pods in the background and closes the sheet; refused while
    /// another sweep runs.
    private func start(_ preview: KubeSanitizePreview) {
        guard let client = model.client else { return }
        let names = sanitizeNames(preview.pods, selected: selected)
        guard !names.isEmpty else { return }
        failure = nil
        if job.start(client: client, context: model.activeContext, namespace: namespace, names: names) {
            dismiss()
        } else {
            failure = String(localized: "A dead pod cleanup is already running")
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
