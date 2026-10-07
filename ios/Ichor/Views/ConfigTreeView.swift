import SwiftUI
import IchorCore
import UniformTypeIdentifiers

/// What a tree row asks for in edit mode.
enum ConfigTreeAction {
    case edit(ConfigNode)
    case add(ConfigNode)
    case remove(ConfigNode)
}

/// A node of one document: the same path exists in several documents.
struct ConfigTreeTarget: Identifiable {
    let doc: Int
    let node: ConfigNode

    var id: String { "\(doc)/\(node.id)" }
}

private enum ConfigTreeSheet: Identifiable {
    case field(ConfigTreeTarget)
    case add(ConfigTreeTarget)

    var id: String {
        switch self {
        case .field(let target): "field/\(target.id)"
        case .add(let target): "add/\(target.id)"
        }
    }
}

/// What every row of a document needs to know.
struct ConfigTreeContext {
    let editing: Bool
    /// Secrets are shown in clear: a copied value expires from the clipboard.
    let revealed: Bool
    /// Ids of the nodes a search keeps; nil without a search.
    let visible: Set<String>?
    let act: (ConfigTreeAction) -> Void

    func shows(_ node: ConfigNode) -> Bool { visible?.contains(node.id) ?? true }
}

/// The machine config as a tree, one section per document, described by the Talos schema
/// when there is one. In edit mode the rows open the field sheets.
struct ConfigTreeView: View {
    let tree: ConfigTree
    let query: String
    let editing: Bool
    let revealed: Bool
    /// Applies one edit to the draft; returns why it was refused.
    let apply: (ConfigEdit) async -> String?
    let refresh: () async -> Void

    @State private var sheet: ConfigTreeSheet?
    @State private var removing: ConfigTreeTarget?
    @State private var message: String?

    private var needle: String { query.trimmingCharacters(in: .whitespaces) }

    private func visibleIDs(_ document: ConfigDocument) -> Set<String>? {
        needle.isEmpty ? nil : configVisibleIDs(document.node, needle: needle)
    }

    private var nothingMatches: Bool {
        !needle.isEmpty && tree.documents.allSatisfy { visibleIDs($0)?.isEmpty == true }
    }

    var body: some View {
        List {
            if !tree.schema {
                Section { Text("Field descriptions are unavailable (schema not downloaded)").note() }
            }
            ForEach(tree.documents) { document in
                let visible = visibleIDs(document)
                if visible?.isEmpty != true {
                    documentSection(document, visible: visible)
                }
            }
        }
        .overlay {
            if nothingMatches { ContentUnavailableView.search(text: query) }
        }
        .refreshable { await refresh() }
        .themedBackground()
        .sheet(item: $sheet) { sheet in
            switch sheet {
            case .field(let target): ConfigFieldEditorSheet(doc: target.doc, node: target.node, apply: apply)
            case .add(let target): ConfigAddFieldSheet(doc: target.doc, parent: target.node, apply: apply)
            }
        }
        .confirmationDialog(Text("Remove \(removing?.node.key ?? "")?"), isPresented: $removing.isPresent(),
                            titleVisibility: .visible, presenting: removing) { target in
            Button("Remove", role: .destructive) {
                Task { message = await apply(.remove(doc: target.doc, path: target.node.path)) }
            }
            Button("Cancel", role: .cancel) {}
        } message: { _ in
            Text("It leaves the draft with everything it contains.")
        }
        .messageAlert($message)
    }

    private func documentSection(_ document: ConfigDocument, visible: Set<String>?) -> some View {
        let context = ConfigTreeContext(editing: editing, revealed: revealed, visible: visible) { action in
            handle(action, doc: document.index)
        }
        let root = document.node
        return Section {
            ForEach(root.children ?? []) { child in
                ConfigNodeRow(node: child, context: context, inList: root.type == "array")
            }
        } header: {
            HStack {
                Text(verbatim: document.title)
                Spacer()
                if editing && root.canAdd {
                    Button { handle(.add(root), doc: document.index) } label: {
                        Label(root.addLabel, systemImage: "plus").labelStyle(.iconOnly)
                    }
                }
            }
            .textCase(nil)
        }
    }

    private func handle(_ action: ConfigTreeAction, doc: Int) {
        switch action {
        case .edit(let node): sheet = .field(ConfigTreeTarget(doc: doc, node: node))
        case .add(let node): sheet = .add(ConfigTreeTarget(doc: doc, node: node))
        case .remove(let node): removing = ConfigTreeTarget(doc: doc, node: node)
        }
    }
}

extension ConfigNode {
    /// A list takes items; an object takes the schema's missing fields or free keys.
    var canAdd: Bool {
        guard !isRedacted else { return false }
        if type == "array" { return true }
        return type == "object" && (!(addable ?? []).isEmpty || !(freeKeyType ?? "").isEmpty)
    }

    var addLabel: String {
        type == "array" ? String(localized: "Add item") : String(localized: "Add field")
    }
}

/// One node and, for an object or a list, what it contains.
struct ConfigNodeRow: View {
    let node: ConfigNode
    let context: ConfigTreeContext
    var inList = false

    @State private var expanded = false
    @State private var showsAll = false

    var body: some View {
        if context.shows(node) {
            if node.isContainer && !node.isRedacted {
                DisclosureGroup(isExpanded: expansion) {
                    ForEach(node.children ?? []) { child in
                        ConfigNodeRow(node: child, context: context, inList: node.type == "array")
                    }
                } label: {
                    HStack {
                        details
                        Spacer(minLength: 8)
                        if context.editing { actions }
                    }
                }
            } else if context.editing && !node.isRedacted {
                Button { context.act(.edit(node)) } label: { details.contentShape(Rectangle()) }
                    .buttonStyle(.plain)
            } else {
                details
                    .contentShape(Rectangle())
                    .onTapGesture { showsAll.toggle() }
                    .contextMenu {
                        if !node.isRedacted { Button(action: copyValue) { Label("Copy", systemImage: "doc.on.doc") } }
                    }
            }
        }
    }

    /// A search opens everything it keeps.
    private var expansion: Binding<Bool> {
        Binding(get: { expanded || context.visible != nil }, set: { expanded = $0 })
    }

    private var details: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(verbatim: inList ? "[\(node.key)]" : node.key)
                .font(.callout.monospaced().weight(.medium))
            valueText
            if let description = node.description, !description.isEmpty {
                Text(verbatim: description)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(showsAll ? nil : 2)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    @ViewBuilder private var valueText: some View {
        if node.isRedacted {
            Label("Hidden", systemImage: "lock.fill").font(.callout).foregroundStyle(.secondary)
        } else if node.type == "object" {
            Text("\(node.children?.count ?? 0) fields").font(.caption).foregroundStyle(.secondary)
        } else if node.type == "array" {
            Text("\(node.children?.count ?? 0) items").font(.caption).foregroundStyle(.secondary)
        } else if node.type == "boolean" {
            Text(verbatim: node.value == "true" ? String(localized: "On") : String(localized: "Off")).font(.callout)
        } else if node.type == "null" {
            Text(verbatim: "null").font(.callout.monospaced()).foregroundStyle(.secondary)
        } else {
            Text(verbatim: (node.value ?? "").or("\"\""))
                .font(.callout.monospaced())
                .lineLimit(showsAll ? nil : 3)
        }
    }

    private var actions: some View {
        Menu {
            if node.canAdd {
                Button { context.act(.add(node)) } label: { Label(node.addLabel, systemImage: "plus") }
            }
            Button(role: .destructive) { context.act(.remove(node)) } label: { Label("Remove", systemImage: "trash") }
        } label: {
            Image(systemName: "ellipsis.circle").accessibilityLabel(Text("More actions"))
        }
        .buttonStyle(.borderless)
    }

    /// Device-only pasteboard; a revealed secret expires from it after two minutes.
    private func copyValue() {
        let options: [UIPasteboard.OptionsKey: Any] = context.revealed
            ? [.localOnly: true, .expirationDate: Date().addingTimeInterval(120)]
            : [.localOnly: true]
        UIPasteboard.general.setItems([[UTType.utf8PlainText.identifier: node.value ?? ""]], options: options)
    }
}

/// Ids of the nodes a search shows: the ones whose key or value contains `needle`
/// (case-insensitive), all they contain, and their ancestors.
func configVisibleIDs(_ node: ConfigNode, needle: String) -> Set<String> {
    if configNodeMatches(node, needle: needle) { return configSubtreeIDs(node) }
    let below = (node.children ?? []).reduce(into: Set<String>()) { $0.formUnion(configVisibleIDs($1, needle: needle)) }
    return below.isEmpty ? below : below.union([node.id])
}

private func configNodeMatches(_ node: ConfigNode, needle: String) -> Bool {
    if node.key.range(of: needle, options: .caseInsensitive) != nil { return true }
    guard !node.isRedacted, let value = node.value else { return false }
    return value.range(of: needle, options: .caseInsensitive) != nil
}

private func configSubtreeIDs(_ node: ConfigNode) -> Set<String> {
    (node.children ?? []).reduce(into: Set([node.id])) { $0.formUnion(configSubtreeIDs($1)) }
}
