import SwiftUI
import IchorCore

/// Whose diff a `FluxDiffView` shows: a Flux Kustomization, or an Argo CD Application.
enum GitOpsDiffTarget: Hashable {
    case flux(FluxRef)
    case argo(namespace: String, name: String)

    var name: String {
        switch self {
        case .flux(let ref): ref.name
        case .argo(_, let name): name
        }
    }

    var isArgo: Bool {
        if case .argo = self { return true }
        return false
    }

    /// The app whose source "Open source file" searches.
    var app: GitOpsAppRef {
        switch self {
        case .flux(let ref): .flux(kind: ref.kind, namespace: ref.namespace, name: ref.name)
        case .argo(let namespace, let name): .argo(namespace: namespace, name: name)
        }
    }

    /// The short form of a revision: Flux's "branch@sha1:…", or Argo CD's commit list.
    func short(_ revision: String) -> String {
        isArgo ? shortRevisions(revision) : fluxShortRevision(revision)
    }
}

/// What reconciling a Flux Kustomization (or syncing an Argo CD Application) now would change,
/// like `flux diff kustomization` or `argocd app diff`: the revisions compared, a count per
/// kind of change, then each object that would change with its diff (the first few unfolded),
/// and the unchanged ones folded. Built in the cluster with a server-side dry run: nothing is
/// written.
struct FluxDiffView: View {
    let target: GitOpsDiffTarget
    /// The object ("kind/namespace/name", from its page or a failed sync) scrolled to, unfolded.
    let focus: String

    // Explicit: the private @State properties make the memberwise init private.
    init(target: FluxRef, focus: String = "") {
        self.target = .flux(target)
        self.focus = focus
    }

    init(argo namespace: String, name: String, focus: String = "") {
        self.target = .argo(namespace: namespace, name: name)
        self.focus = focus
    }

    @Environment(AppModel.self) private var model
    @Environment(GitHostStore.self) private var hosts
    @State private var state: LoadState<FluxDiff> = .loading
    /// Where the app's files are, for "Open source file"; nil until read or when unreadable.
    @State private var gitSources: GitOpsSource?
    /// The object whose source file is being looked for.
    @State private var searching: String?
    /// The one file found, opened at its line.
    @State private var opened: SourceFileTarget?
    /// Several files declare the object: the user picks one.
    @State private var choosing: SourceFileChoice?
    /// The manifest opened in the editor with an object's live values written ("Keep in Git").
    @State private var keeping: SourceKeepTarget?
    @State private var message: String?
    /// Per object id: what the user folded or unfolded survives a refresh.
    @State private var expanded: [String: Bool] = [:]
    @State private var showUnchanged = false
    /// The focus was scrolled to once: a refresh leaves the scroll alone.
    @State private var scrolledToFocus = false

    /// Unfolded at first: the first objects that would change, so one change shows at a glance.
    private static let unfoldedAtFirst = 3

    var body: some View {
        LoadStateView(state: state, retry: load) { diff in content(diff) }
            .task(id: key) { await load() }
            .task(id: key) { await loadSources() }
            .navigationDestination(item: $opened) { file in
                SourceFileView(source: file.source, path: file.path, app: file.app, line: file.line)
            }
            .navigationDestination(item: $keeping) { file in
                SourceEditView(source: file.source, path: file.path, app: file.app, quickEdit: file.edit)
            }
            .confirmationDialog(
                String(localized: "Defined in several files"), isPresented: $choosing.isPresent(),
                titleVisibility: .visible, presenting: choosing
            ) { choice in
                ForEach(choice.matches) { match in
                    Button(String(localized: "Defined in \(match.path), line \(String(match.line))")) {
                        if let kept = choice.keep {
                            keeping = SourceKeepTarget(
                                source: choice.source, path: match.path, app: target.app,
                                edit: .keep(documentIndex: match.documentIndex, resource: kept))
                        } else {
                            opened = SourceFileTarget(
                                source: choice.source, path: match.path, app: target.app, line: match.line)
                        }
                    }
                }
                Button("Cancel", role: .cancel) {}
            }
            .messageAlert($message)
            .navigationTitle("Diff")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .principal) {
                    VStack(spacing: 0) {
                        Text("Diff").font(.headline)
                        Text(verbatim: target.name).font(.caption).foregroundStyle(.secondary)
                    }
                }
            }
            .allowsLandscape()
    }

    private func content(_ diff: FluxDiff) -> some View {
        let changed = diff.changed
        let unchanged = diff.unchanged
        let focused = focus.isEmpty ? nil : changed.first(where: { $0.focusKey == focus })
        return ScrollViewReader { proxy in
            List {
                Section {
                    if !diff.revision.isEmpty {
                        LabeledContent("Built from") {
                            Text(verbatim: target.short(diff.revision)).font(.callout.monospaced())
                        }
                    }
                    if !diff.applied.isEmpty {
                        LabeledContent("Last applied") {
                            Text(verbatim: target.short(diff.applied))
                                .font(.callout.monospaced())
                                .foregroundStyle(diff.newRevision ? Color.statusWarn : Color.secondary)
                        }
                    }
                    if diff.inSync {
                        Label(
                            "Nothing would change: the cluster matches the source.",
                            systemImage: "checkmark.circle.fill"
                        )
                        .foregroundStyle(.statusOK)
                    }
                    // Opened for one object that has nothing to change: say so, rather than leave it to be looked for.
                    if !focus.isEmpty, focused == nil {
                        let parts = focus.split(separator: "/", omittingEmptySubsequences: false).map(String.init)
                        Label(
                            "\(parts.first ?? "") \(parts.last ?? "") matches what Git would apply: nothing to keep.",
                            systemImage: "checkmark.circle.fill"
                        )
                        .foregroundStyle(.statusOK)
                    }
                    if !diff.counts.isEmpty {
                        ChipFlow {
                            ForEach(diff.counts, id: \.change) { DiffChangeBadge(change: $0.change, count: $0.count) }
                        }
                    }
                    ForEach(diff.warnings, id: \.self) { warning in
                        Label {
                            Text(verbatim: warning)
                        } icon: {
                            Image(systemName: "exclamationmark.triangle.fill")
                        }
                        .font(.callout)
                        .foregroundStyle(.statusWarn)
                    }
                }
                if !changed.isEmpty {
                    Section {
                        ForEach(Array(changed.enumerated()), id: \.element.id) { index, resource in
                            DiffResourceRow(
                                resource: resource, expanded: binding(for: resource, at: index),
                                openSource: openSource(for: resource), searching: searching == resource.id,
                                keepLive: keepLive(for: resource)
                            )
                            .id(resource.id)
                        }
                    }
                }
                if !unchanged.isEmpty {
                    Section {
                        DisclosureGroup(isExpanded: $showUnchanged) {
                            ForEach(unchanged) { r in
                                LabeledContent {
                                    Text(verbatim: r.namespace.isEmpty ? r.name : "\(r.namespace)/\(r.name)")
                                        .font(.callout.monospaced())
                                        .lineLimit(1)
                                        .truncationMode(.middle)
                                } label: {
                                    Text(verbatim: r.kind).foregroundStyle(.secondary)
                                }
                            }
                        } label: {
                            Text("\(unchanged.count) unchanged objects")
                        }
                    }
                }
                Section {
                    EmptyView()
                } footer: {
                    if target.isArgo {
                        Text(
                            "What Argo CD itself compared last: the live state and the state after a sync, read from Argo CD’s cache through a port-forward. Nothing ran in the cluster, nothing was changed. Secret values are hidden."
                        )
                    } else {
                        Text(
                            "Built in the cluster as Flux builds it, then sent to the API server as a server-side apply dry run: nothing was changed. Secret values are hidden."
                        )
                    }
                }
            }
            .refreshable { await load() }
            .themedBackground()
            .task(id: focused?.id) {
                guard let focused, !scrolledToFocus else { return }
                scrolledToFocus = true
                proxy.scrollTo(focused.id, anchor: .top)
            }
        }
    }

    private func binding(for resource: KubeDiffResource, at index: Int) -> Binding<Bool> {
        let initial = resource.focusKey == focus || index < Self.unfoldedAtFirst && resource.change.isChange
        return Binding(
            get: { expanded[resource.id] ?? initial },
            set: { expanded[resource.id] = $0 }
        )
    }

    /// The search for the object's source file, when the app has a browsable Git source and the
    /// object can be in it (a deleted one no longer is).
    private func openSource(for resource: KubeDiffResource) -> (() -> Void)? {
        guard let browsable = gitSources?.firstBrowsable, resource.change != .deleted else { return nil }
        return { Task { await findSource(of: resource, in: browsable.source, index: browsable.index) } }
    }

    /// "Keep live values in Git", when the object has values changed by hand and the app a
    /// browsable Git source to find its manifest in.
    private func keepLive(for resource: KubeDiffResource) -> (() -> Void)? {
        guard let browsable = gitSources?.firstBrowsable, !resource.liveChanges.isEmpty else { return nil }
        return { Task { await findSource(of: resource, in: browsable.source, index: browsable.index, keep: true) } }
    }

    /// keep: the search is for "Keep in Git" (the editor opens) rather than to open the file.
    private func findSource(of resource: KubeDiffResource, in source: GitSource, index: Int, keep: Bool = false) async {
        guard let client = model.client, searching == nil else { return }
        searching = resource.id
        defer { searching = nil }
        let app = target.app
        let demo = model.activeSummary?.demo == true
        do {
            let found = try await client.findSource(
                app: app, source: source, sourceIndex: index, resKind: resource.kind,
                resNamespace: resource.namespace, resName: resource.name,
                hosts: hosts, demo: demo)
            switch found.outcome {
            case .open(let match) where keep:
                keeping = SourceKeepTarget(
                    source: source, path: match.path, app: app,
                    edit: .keep(documentIndex: match.documentIndex, resource: resource))
            case .open(let match):
                opened = SourceFileTarget(source: source, path: match.path, app: app, line: match.line)
            case .choose(let matches):
                choosing = SourceFileChoice(source: source, matches: matches, keep: keep ? resource : nil)
            case .none(let note):
                message =
                    note.isEmpty
                    ? String(localized: "No file of the source declares \(resource.kind) \(resource.name).") : note
            }
        } catch {
            // A missing token: GitHostMissing names the Git host to add one for.
            message = error.localizedDescription
        }
    }

    /// The app's Git sources: without them the diff offers no "Open source file".
    private func loadSources() async {
        guard let client = model.client else { return }
        let app = target.app
        // A failed or slow read must not leave the previous cluster's source to search in.
        gitSources = nil
        gitSources = try? await client.gitOpsSource(
            tool: app.tool, kind: app.kind, namespace: app.namespace, name: app.name)
    }

    /// What a reload keys on: the cluster and the tool's own key (its polling scope).
    private var key: String { target.isArgo ? model.argoKey : model.fluxKey }

    private func load() async {
        guard let client = model.client else { return }
        let key = self.key
        let target = self.target
        let loaded: LoadState<FluxDiff> = await .from {
            switch target {
            case .flux(let ref): try await client.fluxDiff(of: ref)
            case .argo(let namespace, let name): try await client.argoDiff(namespace: namespace, name: name)
            }
        }
        if key == self.key { state = state.refreshed(with: loaded) }
    }
}

/// A source file opened from the diff, at the line declaring the object.
struct SourceFileTarget: Hashable {
    let source: GitSource
    let path: String
    let app: GitOpsAppRef
    let line: Int
}

/// A manifest opened in the editor with an object's live values written by edit.
struct SourceKeepTarget: Hashable {
    let source: GitSource
    let path: String
    let app: GitOpsAppRef
    let edit: QuickEdit
}

/// The files declaring an object, when there are several; keep: the object whose live values
/// go to the one picked.
struct SourceFileChoice: Hashable {
    let source: GitSource
    let matches: [ManifestMatch]
    var keep: KubeDiffResource?
}
