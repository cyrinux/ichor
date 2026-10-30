import SwiftUI
import IchorCore

/// The columns of the server's Table for the resource listed, as its pages bring them.
@Observable
@MainActor
final class KubeResourceColumns {
    private(set) var columns: [KubeResourceColumn] = []

    func update(_ columns: [KubeResourceColumn]) {
        if !columns.isEmpty && columns != self.columns { self.columns = columns }
    }
}

/// The objects of one kind, page by page, with the columns `kubectl get` prints (the server's
/// Table: no code per kind), "wide" adding the `-o wide` ones. A row reads on a phone: its name
/// first, then each cell as "column value", status words tinted. The namespace scope is the
/// Kubernetes screen's; a cluster-scoped kind has none. Tap an object for its YAML and events.
struct KubeResourceListView: View {
    let resource: KubeAPIResource

    @Environment(AppModel.self) private var model
    @Environment(\.scenePhase) private var scenePhase
    @State private var list: PagedList<KubeResourceRow>
    @State private var columns: KubeResourceColumns
    @State private var scope = KubeBrowserScope()
    @State private var query = ""
    /// The row whose Delete… was chosen from its context menu.
    @State private var deleting: KubeResourceRow?
    @AppStorage("kubeBrowser.wide") private var wide = false

    init(resource: KubeAPIResource) {
        self.resource = resource
        let columns = KubeResourceColumns()
        _columns = State(initialValue: columns)
        _list = State(
            initialValue: PagedList<KubeResourceRow>(base: "browser|\(resource.id)", persist: false) {
                client, namespace, token in
                let page = try await client.resourcePage(resource, namespace: namespace, token: token)
                await columns.update(page.columns)
                return page.page
            })
    }

    var body: some View {
        let control =
            resource.namespaced
            ? scope.control(model: model) : KubeScopeControl(scope: KubeScope(), namespaces: nil) { _ in }
        KubeListFrame(
            control: control, list: list, query: query, namespaces: resourceRowNamespaces, scoped: resource.namespaced
        ) { load in
            rows(load, showNamespace: resource.namespaced && control.scope.namespace == nil)
        }
        .searchable(text: $query, prompt: Text("Name or any column"))
        .autocorrectionDisabled()
        .textInputAutocapitalization(.never)
        .navigationTitle(Text(verbatim: resource.kind))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if hasWideColumns(columns.columns) {
                ToolbarItem(placement: .primaryAction) {
                    Toggle(isOn: $wide) {
                        Label("Wide", systemImage: wide ? "rectangle.expand.vertical" : "rectangle.compress.vertical")
                    }
                    .toggleStyle(.button)
                }
            }
        }
        .sheet(item: $deleting) { row in
            KubeRowDeleteSheet(resource: resource, row: row) {
                Task { await list.refresh(model: model) }
            }
        }
        .task(id: kubeNamespacesKey(model)) {
            if resource.namespaced { await scope.loadNamespaces(model: model) }
        }
        .task(id: LiveTrigger(source: kubeNamespacesKey(model), settles: list.settles, active: scenePhase == .active)) {
            await follow()
        }
    }

    private struct LiveTrigger: Hashable {
        let source: String
        let settles: Int
        let active: Bool
    }

    /// Keeps the list live while on screen and the app active, as SelectedPodsView does: the
    /// Go core's watch replaces the rows and the columns on SYNC, then adds, updates and removes
    /// rows by namespace/name. It starts once a load settled, and over again after each one.
    /// Followed only when a namespace is chosen or the load holds every object, so the watch
    /// never lists more than what was already shown; events never (their own stream).
    private func follow() async {
        guard scenePhase == .active, list.settles > 0, resource.resource != "events",
            case .loaded(let load, _, _) = list.state, let shown = list.scope
        else { return }
        let namespace = resource.namespaced ? shown.namespace : nil
        guard namespace != nil || !load.hasMore else { return }
        while !Task.isCancelled {
            guard let client = model.client else { return }
            for await event in client.resourceWatch(resource, namespace: namespace) {
                if case .update(let watched) = event {
                    if let changed = watched.columns { columns.update(changed) }
                    list.apply { $0.applying(watched.change, key: \.id) }
                }
            }
            try? await Task.sleep(for: .seconds(kubeWatchRetrySeconds))
        }
    }

    private func rows(_ load: PagedLoad<KubeResourceRow>, showNamespace: Bool) -> some View {
        // In the server's order: sorting would move rows as pages arrive.
        let shown = filterResourceRows(load.items, query: query)
        let indices = browserColumnIndices(columns.columns, wide: wide)
        let now = Date()
        return List {
            Section {
                ForEach(shown) { row in
                    NavigationLink {
                        KubeObjectView(resource: resource, namespace: row.namespace, name: row.name) {
                            Task { await list.refresh(model: model) }
                        }
                    } label: {
                        KubeResourceRowView(
                            row: row, columns: columns.columns, indices: indices, showNamespace: showNamespace, now: now
                        )
                    }
                    .contextMenu { rowMenu(row) }
                }
                if load.hasMore && query.isEmpty { LoadMoreRow { list.loadMore(model: model) } }
            } header: {
                if !shown.isEmpty {
                    Text(verbatim: load.done ? "\(shown.count)" : "\(shown.count)+")
                }
            }
        }
        .emptyOverlay(shown.isEmpty, query: query) {
            ContentUnavailableView {
                Label {
                    Text(verbatim: resource.kind)
                } icon: {
                    Image(systemName: "square.grid.3x3")
                }
            } description: {
                Text("Nothing here.")
            }
        }
        .refreshable { await list.refresh(model: model) }
        .themedBackground()
    }

    /// A row's long press: its name to the clipboard, its link when the kind has one, and
    /// Delete…, which opens the object screen's preview sheet (no swipe: the row is a link and
    /// a Delete swipe on any object is too easy to hit).
    @ViewBuilder private func rowMenu(_ row: KubeResourceRow) -> some View {
        Button {
            UIPasteboard.general.string = row.name
        } label: {
            Label("Copy name", systemImage: "doc.on.doc")
        }
        if let target = resource.shareTarget(namespace: row.namespace, name: row.name) {
            ShareLinkButton(target: target)
        }
        if resource.verbs.contains("delete") {
            Button(role: .destructive) {
                deleting = row
            } label: {
                Label("Delete…", systemImage: "trash")
            }
        }
    }
}

/// The object screen's delete sheet for a browser row: the credentials' delete can-I is asked
/// here, as the object screen asks it, and the sheet reads its preview itself.
private struct KubeRowDeleteSheet: View {
    let resource: KubeAPIResource
    let row: KubeResourceRow
    let onDeleted: () -> Void

    @Environment(AppModel.self) private var model
    @State private var denial: KubeAccess?

    var body: some View {
        KubeObjectDeleteSheet(
            resource: resource, namespace: row.namespace, name: row.name, denial: denial, onDeleted: onDeleted
        )
        .task { await loadDenial() }
    }

    /// An answer that cannot be had leaves Delete offered: the API server still decides.
    private func loadDenial() async {
        guard let client = model.client,
            let access = try? await client.kubeCan(
                verb: "delete", group: resource.group, resource: resource.resource,
                namespace: row.namespace, name: row.name),
            !Task.isCancelled
        else { return }
        denial = access.isDenied ? access : nil
    }
}

/// One object: its name (monospaced, prominent), age and pending deletion on the same line,
/// its namespace when every one is listed, then its cells as "column value" pairs.
private struct KubeResourceRowView: View {
    let row: KubeResourceRow
    let columns: [KubeResourceColumn]
    let indices: [Int]
    let showNamespace: Bool
    let now: Date

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Text(verbatim: row.name)
                    .font(.callout.monospaced().weight(.medium))
                    .lineLimit(1)
                    .truncationMode(.middle)
                Spacer(minLength: 4)
                if row.deleting {
                    Text("Deleting")
                        .font(.caption2.weight(.semibold))
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .foregroundStyle(.statusWarn)
                        .background(Color.statusWarn.opacity(0.15), in: Capsule())
                }
                if row.created > 0 {
                    Text(verbatim: localizedDuration(max(0, Int64(now.timeIntervalSince1970) - row.created)))
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .monospacedDigit()
                }
            }
            if showNamespace && !row.namespace.isEmpty {
                Text(verbatim: row.namespace).font(.caption).foregroundStyle(.secondary)
            }
            let cells = browserCells(row, columns: columns, indices: indices)
            if !cells.isEmpty {
                cellsText(cells)
                    .font(.caption)
                    .lineLimit(4)
            }
        }
        .padding(.vertical, 1)
    }

    /// "Ready 1/1 · Status Running · Restarts 0": wraps as a phone needs, labels muted.
    private func cellsText(_ cells: [(column: KubeResourceColumn, value: String)]) -> Text {
        cells.enumerated().reduce(Text(verbatim: "")) { text, item in
            let (column, value) = item.element
            let shown = browserCellAge(value, type: column.type, now: now).map(localizedDuration) ?? value
            let tone = kubeCellTone(column: column.name, value: value)
            let separator =
                item.offset == 0 ? Text(verbatim: "") : Text(verbatim: "  ·  ").foregroundStyle(Color.secondary)
            let label = Text(verbatim: "\(column.name) ").foregroundStyle(Color.secondary)
            let valueText = Text(verbatim: shown).foregroundStyle(tone.color ?? Color.primary)
            return text + separator + label + valueText
        }
    }
}
