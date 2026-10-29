import SwiftUI
import IchorCore

/// The active cluster's Updates, from the cluster menu's "Updates…": its images and charts with
/// a newer patch, minor or major version, what already bumps them, and those not comparable with
/// why. The first visit on a cluster asks once before the registries are asked. A candidate's
/// chip opens its file with the quick edit prefilled, then the pull request. Same screen as
/// Android's UpdatesScreen.kt.
struct UpdatesView: View {
    @Environment(AppModel.self) private var model
    @Environment(GitHostStore.self) private var hosts
    @State private var session = UpdatesSession.shared
    @State private var state: LoadState<ClusterUpdates> = .loading
    @State private var consenting = false
    /// The row whose file is being found.
    @State private var resolving: String?
    /// Why a tap led nowhere, by row.
    @State private var notes: [String: String] = [:]
    @State private var opened: UpdateFileRoute?
    @State private var editing: UpdateEditRoute?
    @State private var renderedHint = false

    private var demo: Bool { model.activeSummary?.demo == true }
    private var key: String { UpdatesSession.key(model) }

    var body: some View {
        content
            .navigationTitle("Updates")
            .toolbar {
                ToolbarItem(placement: .primaryAction) {
                    Button {
                        Task { await fetch() }
                    } label: {
                        Label("Refresh", systemImage: "arrow.clockwise")
                    }
                    .disabled(consenting)
                }
            }
            .task(id: key) { start() }
            .alert("Ask the registries?", isPresented: $consenting) {
                Button("Ask") {
                    session.consent(key)
                    Task { await fetch() }
                }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text(
                    "The app asks the image registries which tags exist; your pull secrets are used for private registries."
                )
            }
            .alert("Rendered by a chart: set the tag in its values", isPresented: $renderedHint) {
                Button("OK", role: .cancel) {}
            }
            .navigationDestination(item: $opened) { file in
                SourceFileView(
                    source: file.source, path: file.path, app: file.app, line: file.line > 0 ? file.line : nil,
                    prefill: file.prefill)
            }
            .navigationDestination(item: $editing) { file in
                SourceEditView(source: file.source, path: file.path, app: file.app, quickEdit: file.edit)
            }
    }

    @ViewBuilder private var content: some View {
        switch state {
        case .loading:
            VStack(alignment: .leading, spacing: 8) {
                ProgressView().progressViewStyle(.linear)
                let queried = session.results[key]?.lookups.queried ?? 0
                Group {
                    if queried > 0 {
                        Text("Asking the registries (images and charts: \(queried))…")
                    } else {
                        Text("Asking the registries…")
                    }
                }
                .font(.caption).foregroundStyle(.secondary)
                Spacer()
            }
            .padding()
        case .failed(let message):
            ContentUnavailableView {
                Label("Updates", systemImage: "exclamationmark.triangle")
            } description: {
                Text(verbatim: message)
            } actions: {
                Button("Retry") { Task { await fetch() } }
            }
        case .loaded(let updates, _, _):
            list(updates)
        }
    }

    private func list(_ updates: ClusterUpdates) -> some View {
        let sections = updateSections(updates)
        return List {
            if updates.lookups.failed > 0 || !updates.skipped.isEmpty {
                Section {
                    if updates.lookups.failed > 0 {
                        Text("Lookups failed: \(updates.lookups.failed) of \(updates.lookups.queried)")
                    }
                    ForEach(updates.skipped, id: \.what) { skipped in
                        Text("Skipped: \(skipped.what) (\(skipped.reason))")
                    }
                }
                .font(.caption).foregroundStyle(.secondary)
            }
            if sections.images.isEmpty, sections.charts.isEmpty {
                Section {
                    Label("Everything runs the newest version the policy allows", systemImage: "checkmark.circle")
                        .foregroundStyle(.secondary)
                }
            }
            rows("Images", sections.images)
            rows("Charts", sections.charts)
            rows("Not comparable", sections.notComparable)
        }
        .refreshable { await fetch() }
    }

    @ViewBuilder private func rows(_ title: LocalizedStringKey, _ rows: [UpdateRow]) -> some View {
        if !rows.isEmpty {
            Section(title) {
                ForEach(rows) { row in
                    UpdateRowView(row: row, resolving: resolving == row.id, note: notes[row.id]) { version in
                        Task { await pick(row, version: version) }
                    }
                }
            }
        }
    }

    /// The session's last answer, else a lookup once the notice was accepted for the cluster.
    private func start() {
        notes = [:]
        if let last = session.results[key] {
            state = .loaded(last, at: Date(timeIntervalSince1970: TimeInterval(last.at) / 1000))
        } else if session.hasConsented(key) {
            Task { await fetch() }
        } else {
            consenting = true
        }
    }

    private func fetch() async {
        guard let client = model.client else { return }
        if session.results[key] == nil { state = .loading }
        let key = self.key
        do {
            let updates = try await client.clusterUpdates(repo: model.activeClusterRepo, hosts: hosts, demo: demo)
            session.store(updates, for: key)
            state = .loaded(updates, at: Date())
        } catch {
            state = state.refreshed(with: .failed(error.localizedDescription))
        }
    }

    /// The candidate version of row tapped: finds its file, then opens it (or the editor), or
    /// says on the row why not.
    private func pick(_ row: UpdateRow, version: String) async {
        guard let client = model.client, resolving == nil, row.editable else { return }
        resolving = row.id
        notes[row.id] = nil
        defer { resolving = nil }
        do {
            let target = try await client.updateEditTarget(
                row, version: version, repo: model.activeClusterRepo, hosts: hosts, demo: demo)
            guard let app = target.app?.ref, let source = target.source, !target.path.isEmpty else {
                notes[row.id] = target.note
                return
            }
            let edit = updateQuickEdit(target, row: row, version: version)
            if let edit, edit.op == .path {
                editing = UpdateEditRoute(source: source, path: target.path, app: app, edit: edit)
            } else {
                renderedHint = edit == nil
                opened = UpdateFileRoute(source: source, path: target.path, app: app, line: target.line, prefill: edit)
            }
        } catch {
            notes[row.id] = error.localizedDescription
        }
    }
}

/// An update's file, at its line, with the image sheet prefilled (nil: a values file to edit by hand).
struct UpdateFileRoute: Hashable {
    let source: GitSource
    let path: String
    let app: GitOpsAppRef
    let line: Int
    let prefill: QuickEdit?
}

/// An update's field edit (a chart version), opened in the editor on its review.
struct UpdateEditRoute: Hashable {
    let source: GitSource
    let path: String
    let app: GitOpsAppRef
    let edit: QuickEdit
}
