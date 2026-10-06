import SwiftUI
import IchorCore

/// What reconciling a Flux Kustomization now would change, like `flux diff kustomization`: the
/// revisions compared, a count per kind of change, then each object that would change with its
/// diff (the first few unfolded), and the unchanged ones folded. Built in the cluster with a
/// server-side dry run: nothing is written.
struct FluxDiffView: View {
    let target: FluxRef

    // Explicit: the private @State properties make the memberwise init private.
    init(target: FluxRef) {
        self.target = target
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
            .task(id: model.fluxKey) { await load() }
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
                    LabeledContent("Built from") { Text(verbatim: fluxShortRevision(diff.revision)).font(.callout.monospaced()) }
                }
                if !diff.applied.isEmpty {
                    LabeledContent("Last applied") {
                        Text(verbatim: fluxShortRevision(diff.applied))
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
                Text("Built in the cluster as Flux builds it, then sent to the API server as a server-side apply dry run: nothing was changed. Secret values are hidden.")
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

    private func load() async {
        guard let client = model.client else { return }
        let key = model.fluxKey
        let loaded: LoadState<FluxDiff> = await .from { try await client.fluxDiff(of: target) }
        if key == model.fluxKey { state = state.refreshed(with: loaded) }
    }
}
