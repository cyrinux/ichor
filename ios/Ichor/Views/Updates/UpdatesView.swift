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
    @Environment(StagedEdits.self) private var staged
    /// Rows are being ticked for one pull request: the version each is proposed with, by row.
    @State private var selecting = false
    @State private var selected: [String: String] = [:]
    @State private var planning = false
    @State private var planError: String?
    @State private var proposal: UpdateProposalRoute?

    private var demo: Bool { model.activeSummary?.demo == true }
    private var key: String { UpdatesSession.key(model) }

    var body: some View {
        content
            .navigationTitle("Updates")
            .toolbar {
                if case .loaded = state {
                    ToolbarItem(placement: .primaryAction) {
                        Button(selecting ? LocalizedStringKey("Cancel") : LocalizedStringKey("Select")) {
                            selecting.toggle()
                            selected = [:]
                            planError = nil
                        }
                    }
                }
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
            .safeAreaInset(edge: .bottom) {
                if selecting { proposeBar }
            }
            .sheet(item: $proposal) { plan in
                StagedChangesView(
                    source: plan.source, target: plan.target, defaultTitle: plan.title, defaultBody: plan.body,
                    notes: plan.notes, editable: false)
            }
    }

    /// The selection's bar: "Propose N updates", and what went wrong planning it.
    private var proposeBar: some View {
        VStack(alignment: .leading, spacing: 6) {
            if let planError {
                Text(verbatim: planError).font(.caption).foregroundStyle(.red)
            }
            HStack(spacing: 8) {
                if planning {
                    ProgressView()
                    Text("Reading the files…").font(.caption).foregroundStyle(.secondary)
                } else {
                    Text("Tick the updates to propose in one pull request").font(.caption).foregroundStyle(.secondary)
                }
                Spacer()
                Button {
                    Task { await proposeSelected() }
                } label: {
                    Text("Propose \(selected.count) updates")
                }
                .buttonStyle(.borderedProminent)
                .disabled(selected.isEmpty || planning)
            }
        }
        .padding()
        .background(.bar)
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
                    UpdateRowView(
                        row: row, resolving: resolving == row.id, note: notes[row.id], selecting: selecting,
                        selectedVersion: selected[row.id], onToggle: { toggle(row) },
                        onPick: { version in
                            if selecting {
                                if row.editable { selected[row.id] = version }
                            } else {
                                Task { await pick(row, version: version) }
                            }
                        })
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

extension UpdatesView {
    /// Ticks row with its smallest step, or unticks it.
    fileprivate func toggle(_ row: UpdateRow) {
        if selected[row.id] != nil {
            selected[row.id] = nil
        } else if let version = defaultSelection(row) {
            selected[row.id] = version
        }
    }

    /// Plans one pull request for the rows ticked (UpdateProposalPlan), stages its files and
    /// opens the staged changes prefilled; the rows left out get their reason as a note.
    fileprivate func proposeSelected() async {
        guard let client = model.client, !planning, case .loaded(let updates, _, _) = state else { return }
        let sections = updateSections(updates)
        let selection = (sections.images + sections.charts).compactMap { row in
            selected[row.id].map { (row: row, version: $0) }
        }
        guard !selection.isEmpty else { return }
        planning = true
        planError = nil
        defer { planning = false }
        do {
            let plan = try await client.updateProposalPlan(
                selection, repo: model.activeClusterRepo, hosts: hosts, demo: demo)
            for skip in plan.skipped where selection.indices.contains(skip.index) {
                notes[selection[skip.index].row.id] = skip.reason
            }
            guard let source = plan.source, !plan.files.isEmpty else { return }
            let key = StagedKey(source: source)
            for file in plan.files { staged.stage(key, file.staged) }
            selecting = false
            selected = [:]
            proposal = UpdateProposalRoute(
                source: source, target: plan.baseRef, title: plan.title, body: plan.body,
                notes: plan.skipped.map { "\($0.item): \($0.reason)" })
        } catch {
            planError = error.localizedDescription
        }
    }
}

/// The staged files of an update plan, with its pull request's title, body and branch.
struct UpdateProposalRoute: Identifiable, Hashable {
    let id = UUID()
    let source: GitSource
    let target: String
    let title: String
    let body: String
    let notes: [String]
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
