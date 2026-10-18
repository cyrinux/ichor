import SwiftUI
import IchorCore

/// A plugin's render tree and the catalog that names the kinds its rows open.
struct PluginTreeData {
    let tree: PluginTree
    let catalog: PluginCatalog
}

/// The installed plugin catalog: the same for every cluster, read once until an imported
/// plugin is turned on or off (reset).
@MainActor
enum PluginCatalogCache {
    private static var value: PluginCatalog?

    static func reset() { value = nil }

    /// The catalog; an empty one when it cannot be read (only the kinds of the opened objects miss it).
    static func load(with client: TalosClient) async -> PluginCatalog {
        if let value { return value }
        guard let read = try? await client.pluginCatalog() else { return PluginCatalog() }
        value = read
        return read
    }
}

/// A plugin's tab in a hub (Data services), or the body of its own screen: its summary, its
/// cards (grouped by namespace when the rows span several), the unreadable parts as a warning.
/// Reads again when `reload` changes: from scratch on another scope (cluster, config), so
/// another cluster's rows never show; in place on a new bump (a refresh).
struct PluginList: View {
    let pluginID: String
    let reload: PluginReload
    /// The plugin's name, once read (for a screen's title).
    var onName: ((String) -> Void)?

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<PluginTreeData> = .loading
    @State private var loaded: PluginReload?
    /// Changes made from the tab (a kube-green schedule saved): each one reads in place.
    @State private var changes = 0
    @State private var expanded: Set<String> = []
    /// Bumped by each read: only the latest one's answer is shown.
    @State private var sequence = 0

    /// The reload asked for, with the changes made from the tab: both only grow, so their sum
    /// changes whenever one does.
    private var current: PluginReload { PluginReload(scope: reload.scope, bump: reload.bump + changes) }

    var body: some View {
        LoadStateView(state: state, retry: { await load(current, refresh: true) }) { data in
            content(data)
        }
        .task(id: current) { await load(current, refresh: false) }
    }

    private func content(_ data: PluginTreeData) -> some View {
        let tree = data.tree
        return List {
            Section { PluginSummaryHeader(summary: tree.summary) }
            if !tree.errorLines.isEmpty {
                Section {
                    Label {
                        Text(verbatim: ([String(localized: "Not everything could be read:")] + tree.errorLines).joined(separator: "\n"))
                    } icon: {
                        Image(systemName: "exclamationmark.triangle")
                    }
                    .font(.callout)
                    .foregroundStyle(Color.statusWarn)
                }
            }
            if !tree.active {
                Section { Text("Not found on this cluster.").note() }
            } else if tree.shownCards.isEmpty {
                Section { Text("Nothing to show.").note() }
            } else {
                ForEach(Array(tree.shownCards.enumerated()), id: \.offset) { _, card in
                    cardSections(card, tree: tree, catalog: data.catalog)
                }
            }
        }
        .refreshable { await load(current, refresh: true) }
        .themedBackground()
    }

    /// A card as one section, or one per namespace (its header) when its rows span several.
    @ViewBuilder
    private func cardSections(_ card: PluginCard, tree: PluginTree, catalog: PluginCatalog) -> some View {
        if card.rows.isEmpty {
            Section { Text(verbatim: card.empty).note() }
        } else {
            let grouped = card.groupsByNamespace
            ForEach(card.rowGroups, id: \.namespace) { group in
                Section {
                    ForEach(group.rows, id: \.index) { item in
                        let key = item.row.expandKey(card: card.id, index: item.index)
                        PluginRowView(
                            row: item.row,
                            // The namespace is the section's header when grouped.
                            showSubtitle: !grouped || item.row.subtitle != item.row.ref?.namespace,
                            expanded: expandedBinding(key),
                            kindOf: { catalog.kindOf(tree.plugin.id, $0) },
                            extra: rowExtra(item.row)
                        )
                    }
                } header: {
                    if grouped { Text(verbatim: group.namespace) }
                }
            }
        }
    }

    /// The native "Edit schedule" under a kube-green plugin row of a SleepInfo, until plugin
    /// actions can carry a form; nil for any other row.
    private func rowExtra(_ row: PluginRow) -> AnyView? {
        guard pluginID == kubeGreenPluginID, let ref = row.ref, ref.isKubeGreenSleepInfo else { return nil }
        return AnyView(KubeGreenPluginEdit(ref: ref) { changes += 1 })
    }

    private func expandedBinding(_ key: String) -> Binding<Bool> {
        Binding(
            get: { expanded.contains(key) },
            set: { open in
                if open { expanded.insert(key) } else { expanded.remove(key) }
            }
        )
    }

    /// Reads for `next`: from scratch when its scope changed, nothing was read, or nothing is
    /// shown (a retry after a failed first read shows the loading state); in place when only its
    /// bump did, or on `refresh` (pull to refresh). Only the latest read's answer is shown: an
    /// older one, or one for a scope the screen moved away from, is dropped.
    private func load(_ next: PluginReload, refresh: Bool) async {
        guard let client = model.client else { return }
        let showsData: Bool = { if case .loaded = state { return true } else { return false } }()
        var change: PluginReload.Change = refresh && loaded?.scope == next.scope ? .refresh : next.change(from: loaded)
        if change == .refresh && !showsData { change = .reset }
        switch change {
        case .none: return
        case .reset:
            state = .loading
            expanded = []
        case .refresh: break
        }
        loaded = next
        sequence += 1
        let mine = sequence
        let read: LoadState<PluginTreeData> = await .from {
            let tree = try await client.pluginData(id: pluginID)
            // Only the kinds of the objects its rows open: the rows show without it.
            let catalog = await PluginCatalogCache.load(with: client)
            return PluginTreeData(tree: tree, catalog: catalog)
        }
        guard mine == sequence, loaded?.scope == next.scope else { return }
        state = state.refreshed(with: read)
        if case .loaded(let data, _, _) = read, !data.tree.plugin.name.isEmpty { onName?(data.tree.plugin.name) }
    }
}

/// "Edit schedule" for a SleepInfo shown by the kube-green plugin, with the existing schedule
/// sheet: the SleepInfo as written is read the way the workload sheets read it
/// (KubeGreenStore, kept a minute). Nothing when it cannot be read (refused, or gone).
struct KubeGreenPluginEdit: View {
    let ref: PluginRef
    /// After a save: the tab reads again.
    let onSaved: () -> Void

    @Environment(AppModel.self) private var model
    @State private var info: KubeGreenSleepInfo?
    @State private var failed = false
    @State private var editing: KubeGreenSleepInfo?
    @State private var reads = 0

    var body: some View {
        Group {
            if let info {
                KubeGreenEditButton(info: info) { editing = info }
            } else if !failed {
                Button("Edit schedule") {}.disabled(true)
            }
        }
        .task(id: "\(ref.key)#\(reads)") { await read() }
        .sheet(item: $editing) { info in
            KubeGreenScheduleSheet(info: info) { @MainActor in
                // The workload sheets and the next editor start from the new schedule.
                KubeGreenStore.shared.forget(namespace: ref.namespace, key: model.kubeGreenKey)
                reads += 1
                onSaved()
            }
        }
    }

    private func read() async {
        guard let client = model.client else { return }
        // The one-minute cache the workload sheets share; forgotten after a save.
        let namespace = await KubeGreenStore.shared.load(namespace: ref.namespace, key: model.kubeGreenKey, with: client)
        let found = namespace?.sleepInfos.first { $0.name == ref.name }
        info = found
        failed = found == nil
    }
}
