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
    @State private var list: PagedList<KubeResourceRow>
    @State private var columns: KubeResourceColumns
    @State private var scope = KubeBrowserScope()
    @State private var query = ""
    @AppStorage("kubeBrowser.wide") private var wide = false

    init(resource: KubeAPIResource) {
        self.resource = resource
        let columns = KubeResourceColumns()
        _columns = State(initialValue: columns)
        _list = State(initialValue: PagedList<KubeResourceRow>(base: "browser|\(resource.id)", persist: false) { client, namespace, token in
            let page = try await client.resourcePage(resource, namespace: namespace, token: token)
            await columns.update(page.columns)
            return page.page
        })
    }

    var body: some View {
        let control = resource.namespaced ? scope.control(model: model) : KubeScopeControl(scope: KubeScope(), namespaces: nil) { _ in }
        KubeListFrame(control: control, list: list, query: query, namespaces: resourceRowNamespaces, scoped: resource.namespaced) { load in
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
        .task(id: kubeNamespacesKey(model)) {
            if resource.namespaced { await scope.loadNamespaces(model: model) }
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
                        KubeObjectView(resource: resource, namespace: row.namespace, name: row.name)
                    } label: {
                        KubeResourceRowView(row: row, columns: columns.columns, indices: indices, showNamespace: showNamespace, now: now)
                    }
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
                Label { Text(verbatim: resource.kind) } icon: { Image(systemName: "square.grid.3x3") }
            } description: {
                Text("Nothing here.")
            }
        }
        .refreshable { await list.refresh(model: model) }
        .themedBackground()
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
            let separator = item.offset == 0 ? Text(verbatim: "") : Text(verbatim: "  ·  ").foregroundStyle(Color.secondary)
            let label = Text(verbatim: "\(column.name) ").foregroundStyle(Color.secondary)
            let valueText = Text(verbatim: shown).foregroundStyle(tone.color ?? Color.primary)
            return text + separator + label + valueText
        }
    }
}
