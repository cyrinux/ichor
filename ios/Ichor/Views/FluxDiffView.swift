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

    // Explicit: the private @State properties make the memberwise init private.
    init(target: FluxRef) {
        self.target = .flux(target)
    }

    init(argo namespace: String, name: String) {
        self.target = .argo(namespace: namespace, name: name)
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
    @State private var message: String?
    /// Per object id: what the user folded or unfolded survives a refresh.
    @State private var expanded: [String: Bool] = [:]
    @State private var showUnchanged = false

    /// Unfolded at first: the first objects that would change, so one change shows at a glance.
    private static let unfoldedAtFirst = 3

    var body: some View {
        LoadStateView(state: state, retry: load) { diff in content(diff) }
            .task(id: key) { await load() }
            .task(id: key) { await loadSources() }
            .navigationDestination(item: $opened) { file in
                SourceFileView(source: file.source, path: file.path, app: file.app, line: file.line)
            }
            .confirmationDialog(String(localized: "Defined in several files"), isPresented: $choosing.isPresent(),
                                titleVisibility: .visible, presenting: choosing) { choice in
                ForEach(choice.matches) { match in
                    Button(String(localized: "Defined in \(match.path), line \(String(match.line))")) {
                        opened = SourceFileTarget(source: choice.source, path: match.path, app: target.app, line: match.line)
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
    }

    private func content(_ diff: FluxDiff) -> some View {
        let changed = diff.changed
        let unchanged = diff.unchanged
        return List {
            Section {
                if !diff.revision.isEmpty {
                    LabeledContent("Built from") { Text(verbatim: target.short(diff.revision)).font(.callout.monospaced()) }
                }
                if !diff.applied.isEmpty {
                    LabeledContent("Last applied") {
                        Text(verbatim: target.short(diff.applied))
                            .font(.callout.monospaced())
                            .foregroundStyle(diff.newRevision ? Color.statusWarn : Color.secondary)
                    }
                }
                if diff.inSync {
                    Label("Nothing would change: the cluster matches the source.", systemImage: "checkmark.circle.fill")
                        .foregroundStyle(.statusOK)
                }
                if !diff.counts.isEmpty {
                    ChipFlow {
                        ForEach(diff.counts, id: \.change) { DiffChangeBadge(change: $0.change, count: $0.count) }
                    }
                }
                ForEach(diff.warnings, id: \.self) { warning in
                    Label { Text(verbatim: warning) } icon: { Image(systemName: "exclamationmark.triangle.fill") }
                        .font(.callout)
                        .foregroundStyle(.statusWarn)
                }
            }
            if !changed.isEmpty {
                Section {
                    ForEach(Array(changed.enumerated()), id: \.element.id) { index, resource in
                        DiffResourceRow(resource: resource, expanded: binding(for: resource, at: index),
                                        openSource: openSource(for: resource), searching: searching == resource.id)
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
                    Text("What Argo CD itself compared last: the live state and the state after a sync, read from Argo CD’s cache through a port-forward. Nothing ran in the cluster, nothing was changed. Secret values are hidden.")
                } else {
                    Text("Built in the cluster as Flux builds it, then sent to the API server as a server-side apply dry run: nothing was changed. Secret values are hidden.")
                }
            }
        }
        .refreshable { await load() }
        .themedBackground()
    }

    private func binding(for resource: KubeDiffResource, at index: Int) -> Binding<Bool> {
        let initial = index < Self.unfoldedAtFirst && resource.change.isChange
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

    private func findSource(of resource: KubeDiffResource, in source: GitSource, index: Int) async {
        guard let client = model.client, searching == nil else { return }
        searching = resource.id
        defer { searching = nil }
        let app = target.app, demo = model.activeSummary?.demo == true
        do {
            let found = try await client.findSource(app: app, source: source, sourceIndex: index, resKind: resource.kind,
                                                    resNamespace: resource.namespace, resName: resource.name,
                                                    hosts: hosts, demo: demo)
            switch found.outcome {
            case .open(let match):
                opened = SourceFileTarget(source: source, path: match.path, app: app, line: match.line)
            case .choose(let matches):
                choosing = SourceFileChoice(source: source, matches: matches)
            case .none(let note):
                message = note.isEmpty ? String(localized: "No file of the source declares \(resource.kind) \(resource.name).") : note
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
        gitSources = try? await client.gitOpsSource(tool: app.tool, kind: app.kind, namespace: app.namespace, name: app.name)
    }

    /// What a reload keys on: the cluster and the tool's own key (its polling scope).
    private var key: String { target.isArgo ? model.argoKey : model.fluxKey }

    private func load() async {
        guard let client = model.client else { return }
        let key = self.key, target = self.target
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

/// The files declaring an object, when there are several.
struct SourceFileChoice: Hashable {
    let source: GitSource
    let matches: [ManifestMatch]
}
