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
    @State private var state: LoadState<FluxDiff> = .loading
    /// Per object id: what the user folded or unfolded survives a refresh.
    @State private var expanded: [String: Bool] = [:]
    @State private var showUnchanged = false

    /// Unfolded at first: the first objects that would change, so one change shows at a glance.
    private static let unfoldedAtFirst = 3

    var body: some View {
        LoadStateView(state: state, retry: load) { diff in content(diff) }
            .task(id: key) { await load() }
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
                        DiffResourceRow(resource: resource, expanded: binding(for: resource, at: index))
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
