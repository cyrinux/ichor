import Foundation

// Mirrors go/plugin/tree.go (the render tree, docs/plugins.md "Render tree") and
// go/ichorgo/plugin_api.go / plugin_catalog.go; same rules as Android's model/Plugin.kt.
// Everything is resolved and translated by Go: the app evaluates nothing and knows no CRD. A
// section this build cannot read decodes to `.unknown` and draws nothing, so a newer core
// never breaks an older app.

/// The category of the plugins drawn as Data services tabs.
public let pluginCategoryDataServices = "data-services"

/// One plugin rendered over the cluster (or its samples in the demo).
public struct PluginTree: Decodable, Equatable, Sendable {
    public let plugin: PluginInfo
    public let active: Bool
    /// api, pods, services, inventory or demo; "" when the product is not there.
    public let detectedVia: String
    public let apiVersion: String
    public let health: String
    /// A list stopped at its limit: counts are a lower bound.
    public let partial: Bool
    public let summary: PluginSummary
    public let cards: [PluginCard]
    public let findings: [PluginFinding]
    public let errors: [PluginError]
    /// The plugin's tool buttons (KubePluginTool runs one), in id order; empty when it has none.
    public let tools: [PluginTool]

    public var serviceHealth: ServiceHealth { pluginHealth(health) }

    /// The cards worth drawing: those with rows, or with a text for none.
    public var shownCards: [PluginCard] { cards.filter { !$0.rows.isEmpty || !$0.empty.isEmpty } }

    /// The unreadable parts as lines: "secrets: forbidden".
    public var errorLines: [String] { errors.map(\.line) }

    private enum CodingKeys: String, CodingKey { case plugin, active, detectedVia, apiVersion, health, partial, summary, cards, findings, errors, tools }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        plugin = try c.field(.plugin, PluginInfo())
        active = try c.field(.active, false)
        detectedVia = try c.field(.detectedVia, "")
        apiVersion = try c.field(.apiVersion, "")
        health = try c.field(.health, "")
        partial = try c.field(.partial, false)
        summary = try c.field(.summary, PluginSummary())
        cards = try c.field(.cards, [])
        findings = try c.field(.findings, [])
        errors = try c.field(.errors, [])
        tools = try c.field(.tools, [])
    }
}

/// A tool's button: `label` and `hint` are translated by Go.
public struct PluginTool: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let id: String
    public let label: String
    public let hint: String

    public init(id: String = "", label: String = "", hint: String = "") {
        self.id = id
        self.label = label
        self.hint = hint
    }

    private enum CodingKeys: String, CodingKey { case id, label, hint }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        label = try c.field(.label, "")
        hint = try c.field(.hint, "")
    }
}

/// What a tool returned (KubePluginTool): a `title` toned by `health`, and cards drawn as the tree's are.
public struct PluginToolResult: Decodable, Equatable, Sendable {
    public let title: String
    public let health: String
    public let cards: [PluginCard]

    public var serviceHealth: ServiceHealth { pluginHealth(health) }

    /// The cards worth drawing: those with rows, or with a text for none.
    public var shownCards: [PluginCard] { cards.filter { !$0.rows.isEmpty || !$0.empty.isEmpty } }

    private enum CodingKeys: String, CodingKey { case title, health, cards }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        title = try c.field(.title, "")
        health = try c.field(.health, "")
        cards = try c.field(.cards, [])
    }
}

/// The last tool run of a plugin screen: running while Go works, then its result or its error.
/// `label` is the tool's button text, for the progress and error lines.
public struct PluginToolRun: Equatable, Sendable {
    public let toolID: String
    public let label: String
    public let result: PluginToolResult?
    public let error: String?

    public var running: Bool { result == nil && error == nil }

    public static func started(_ tool: PluginTool) -> PluginToolRun {
        PluginToolRun(toolID: tool.id, label: tool.label, result: nil, error: nil)
    }

    public func finished(_ result: PluginToolResult) -> PluginToolRun {
        PluginToolRun(toolID: toolID, label: label, result: result, error: nil)
    }

    public func failed(_ message: String) -> PluginToolRun {
        PluginToolRun(toolID: toolID, label: label, result: nil, error: message)
    }
}

public struct PluginInfo: Decodable, Equatable, Sendable {
    public let id: String
    public let name: String
    public let icon: String
    public let category: String
    public let version: String
    /// `native:<route>`: a native screen stands in for the generic one; "" otherwise.
    public let screen: String
    public let website: String

    public init(id: String = "", name: String = "", icon: String = "", category: String = pluginCategoryDataServices,
                version: String = "", screen: String = "", website: String = "") {
        self.id = id
        self.name = name
        self.icon = icon
        self.category = category
        self.version = version
        self.screen = screen
        self.website = website
    }

    private enum CodingKeys: String, CodingKey { case id, name, icon, category, version, screen, website }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        name = try c.field(.name, "")
        icon = try c.field(.icon, "")
        category = try c.field(.category, pluginCategoryDataServices)
        version = try c.field(.version, "")
        screen = try c.field(.screen, "")
        website = try c.field(.website, "")
    }
}

/// The tile in the Data services card and the header of the plugin's tab.
public struct PluginSummary: Decodable, Equatable, Sendable {
    public let title: String
    public let health: String
    public let partial: Bool
    public let badges: [PluginBadge]

    public var serviceHealth: ServiceHealth { pluginHealth(health) }

    /// The summary as one line: "2 sleep schedules · 1 asleep · 1 warnings".
    public var line: String {
        ([title] + badges.map(\.text)).filter { !$0.isEmpty }.joined(separator: " · ")
    }

    public init(title: String = "", health: String = "", partial: Bool = false, badges: [PluginBadge] = []) {
        self.title = title
        self.health = health
        self.partial = partial
        self.badges = badges
    }

    private enum CodingKeys: String, CodingKey { case title, health, partial, badges }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        title = try c.field(.title, "")
        health = try c.field(.health, "")
        partial = try c.field(.partial, false)
        badges = try c.field(.badges, [])
    }
}

public struct PluginBadge: Decodable, Equatable, Sendable {
    public let label: String
    public let value: String
    public let tone: String

    /// "1 asleep": the value, then the label.
    public var text: String { [value, label].filter { !$0.isEmpty }.joined(separator: " ") }

    public init(label: String = "", value: String = "", tone: String = "") {
        self.label = label
        self.value = value
        self.tone = tone
    }

    private enum CodingKeys: String, CodingKey { case label, value, tone }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        label = try c.field(.label, "")
        value = try c.field(.value, "")
        tone = try c.field(.tone, "")
    }
}

/// A list card: one row per object (or data service element).
public struct PluginCard: Decodable, Equatable, Sendable, Identifiable {
    public let id: String
    public let type: String
    public let empty: String
    public let rows: [PluginRow]

    private enum CodingKeys: String, CodingKey { case id, type, empty, rows }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        type = try c.field(.type, "list")
        empty = try c.field(.empty, "")
        rows = try c.field(.rows, [])
    }

    /// Grouped by namespace when every row is a namespaced object and they span several namespaces.
    public var groupsByNamespace: Bool {
        let refs = rows.map(\.ref)
        guard !refs.isEmpty, refs.allSatisfy({ $0 != nil }) else { return false }
        let namespaces = Set(refs.compactMap { $0?.namespace })
        return !namespaces.contains("") && namespaces.count > 1
    }

    /// The rows to draw, with their index and id: one group per namespace, in the order they first
    /// appear, when `groupsByNamespace`; else one group titled "".
    public var rowGroups: [PluginRowGroup] {
        var seen: Set<String> = []
        let indexed = rows.enumerated().map { offset, row in
            let id = row.rowID(index: offset, taken: seen)
            seen.insert(id)
            return PluginIndexedRow(index: offset, id: id, row: row)
        }
        guard groupsByNamespace else { return [PluginRowGroup(namespace: "", rows: indexed)] }
        var order: [String] = []
        var byNamespace: [String: [PluginIndexedRow]] = [:]
        for item in indexed {
            let ns = item.row.ref?.namespace ?? ""
            if byNamespace[ns] == nil { order.append(ns) }
            byNamespace[ns, default: []].append(item)
        }
        return order.map { PluginRowGroup(namespace: $0, rows: byNamespace[$0] ?? []) }
    }
}

public struct PluginIndexedRow: Equatable, Sendable {
    public let index: Int
    /// The row's identity across reads: its object's key, so that a row's state (an open
    /// confirmation, a running action) never passes to another object when rows move; its
    /// index when it has no object, or when another row already has that object.
    public let id: String
    public let row: PluginRow

    /// The key of its expanded state, unique within the tree: by its id, so that two rows of
    /// one object (a name's A and AAAA records) open apart.
    public func expandKey(card: String) -> String { "\(card)|\(id)" }
}

/// The rows of one namespace ("" when the card is not grouped).
public struct PluginRowGroup: Equatable, Sendable {
    public let namespace: String
    public let rows: [PluginIndexedRow]
}

/// An object the app can open on its object screen.
public struct PluginRef: Decodable, Equatable, Hashable, Sendable {
    public let group: String
    public let version: String
    public let resource: String
    public let namespace: String
    public let name: String
    /// Set when the plugin's ref names another object than the listed one; "" otherwise.
    public let kind: String

    public init(group: String = "", version: String = "", resource: String = "", namespace: String = "", name: String = "", kind: String = "") {
        self.group = group
        self.version = version
        self.resource = resource
        self.namespace = namespace
        self.name = name
        self.kind = kind
    }

    private enum CodingKeys: String, CodingKey { case group, version, resource, namespace, name, kind }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        group = try c.field(.group, "")
        version = try c.field(.version, "")
        resource = try c.field(.resource, "")
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        kind = try c.field(.kind, "")
    }

    public var key: String { "\(group)/\(resource)/\(namespace)/\(name)" }

    /// "demo-lab/weekend", or the name of a cluster-scoped object.
    public var label: String { [namespace, name].filter { !$0.isEmpty }.joined(separator: "/") }

    /// The object screen's resource; `kind` comes from the ref, else the plugin catalog ("" when
    /// unknown: only the title misses it).
    public func apiResource(kind: String) -> KubeAPIResource {
        KubeAPIResource(group: group, version: version.isEmpty ? "v1" : version, resource: resource, kind: kind, namespaced: !namespace.isEmpty)
    }

    /// A SleepInfo of kube-green: its plugin rows get the native schedule editor.
    public var isKubeGreenSleepInfo: Bool { group == "kube-green.com" && resource == "sleepinfos" }
}

/// The built-in kube-green plugin's id (go/plugin/builtin/kube-green.yaml).
public let kubeGreenPluginID = "kube-green"

public struct PluginRow: Decodable, Equatable, Sendable {
    public let ref: PluginRef?
    public let title: String
    public let subtitle: String
    public let status: PluginStatus?
    public let trailing: String
    public let health: PluginHealth?
    public let detail: [PluginSection]

    private enum CodingKeys: String, CodingKey { case ref, title, subtitle, status, trailing, health, detail }
    private enum TrailingKeys: String, CodingKey { case text }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        ref = try c.decodeIfPresent(PluginRef.self, forKey: .ref)
        title = try c.field(.title, "")
        subtitle = try c.field(.subtitle, "")
        status = try c.decodeIfPresent(PluginStatus.self, forKey: .status)
        if c.contains(.trailing), try !c.decodeNil(forKey: .trailing) {
            trailing = try c.nestedContainer(keyedBy: TrailingKeys.self, forKey: .trailing).field(.text, "")
        } else {
            trailing = ""
        }
        health = try c.decodeIfPresent(PluginHealth.self, forKey: .health)
        detail = try c.field(.detail, [])
    }

    /// The row's tone: its status's, else its health's.
    public var serviceHealth: ServiceHealth { pluginHealth(status?.tone ?? health?.tone ?? "") }

    /// A row with neither status nor health (a data service's) has no dot, not a grey "unknown" one.
    public var showsDot: Bool { status != nil || health != nil }

    /// Whether it opens on details: any section this build can draw.
    public var hasDetail: Bool { detail.contains { !$0.isUnknown } }

    /// The line under the title: the subtitle (left out when `showSubtitle` is false: the
    /// namespace group says it) and the health's message.
    public func subtitleLine(showSubtitle: Bool = true) -> String {
        [showSubtitle ? subtitle : "", health?.message ?? ""].filter { !$0.isEmpty }.joined(separator: " · ")
    }

    /// See PluginIndexedRow.id; `taken` are the ids of the rows before it.
    func rowID(index: Int, taken: Set<String>) -> String {
        if let key = ref?.key, !taken.contains(key) { return key }
        return "#\(index)"
    }

}

public struct PluginStatus: Decodable, Equatable, Sendable {
    public let tone: String
    public let text: String

    private enum CodingKeys: String, CodingKey { case tone, text }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        tone = try c.field(.tone, "")
        text = try c.field(.text, "")
    }
}

public struct PluginHealth: Decodable, Equatable, Sendable {
    public let tone: String
    public let reason: String
    public let message: String

    private enum CodingKeys: String, CodingKey { case tone, reason, message }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        tone = try c.field(.tone, "")
        reason = try c.field(.reason, "")
        message = try c.field(.message, "")
    }
}

/// A detail primitive, discriminated on `type`.
public enum PluginSection: Decodable, Equatable, Sendable {
    case keyValues([PluginItem])
    case badges([PluginItem])
    case gauge(PluginGauge)
    case conditions([PluginCondition])
    case relations([PluginItem])
    case table(PluginTable)
    case text(PluginText)
    case actions([PluginActionItem])
    /// A section this build cannot read (its type, "" when not even that): drawn as nothing.
    case unknown(String)

    private enum CodingKeys: String, CodingKey { case type, items }

    /// Never fails: an entry that is not an object, a type this build does not know, or a known
    /// type whose fields do not decode (a newer or broken core) is `.unknown`, and the rest of
    /// the tree still renders.
    public init(from decoder: Decoder) throws {
        guard let c = try? decoder.container(keyedBy: CodingKeys.self) else {
            self = .unknown("")
            return
        }
        let type = (try? c.decodeIfPresent(String.self, forKey: .type)) ?? ""
        do {
            switch type {
            case "keyValues": self = .keyValues(try c.field(.items, []))
            case "badges": self = .badges(try c.field(.items, []))
            case "gauge": self = .gauge(try PluginGauge(from: decoder))
            case "conditions": self = .conditions(try c.field(.items, []))
            case "relations": self = .relations(try c.field(.items, []))
            case "table": self = .table(try PluginTable(from: decoder))
            case "text": self = .text(try PluginText(from: decoder))
            case "actions": self = .actions(try c.field(.items, []))
            default: self = .unknown(type)
            }
        } catch {
            self = .unknown(type)
        }
    }

    public var isUnknown: Bool {
        if case .unknown = self { return true }
        return false
    }
}

/// A keyValues, badges or relations entry.
public struct PluginItem: Decodable, Equatable, Sendable {
    public let label: String
    public let value: String
    public let tone: String
    public let mono: Bool
    public let ref: PluginRef?
    public let text: String

    /// A badge's chip: "1 asleep", "Deployments".
    public var badgeText: String { [value, label].filter { !$0.isEmpty }.joined(separator: " ") }

    /// A relation's target: its text, else the object's namespace/name.
    public var target: String { text.isEmpty ? (ref?.label ?? "") : text }

    private enum CodingKeys: String, CodingKey { case label, value, tone, mono, ref, text }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        label = try c.field(.label, "")
        value = try c.field(.value, "")
        tone = try c.field(.tone, "")
        mono = try c.field(.mono, false)
        ref = try c.decodeIfPresent(PluginRef.self, forKey: .ref)
        text = try c.field(.text, "")
    }
}

/// A bar with its label and text ("0 B / 512 MiB").
public struct PluginGauge: Decodable, Equatable, Sendable {
    public let label: String
    public let value: Double?
    public let max: Double?
    public let unit: String
    public let tone: String
    public let text: String

    /// 0...1, nil when there is no maximum to measure against.
    public var fraction: Double? {
        guard let value, let max, max > 0 else { return nil }
        return Swift.min(Swift.max(value / max, 0), 1)
    }

    private enum CodingKeys: String, CodingKey { case label, value, max, unit, tone, text }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        label = try c.field(.label, "")
        value = try c.decodeIfPresent(Double.self, forKey: .value)
        max = try c.decodeIfPresent(Double.self, forKey: .max)
        unit = try c.field(.unit, "")
        tone = try c.field(.tone, "")
        text = try c.field(.text, "")
    }
}

public struct PluginTable: Decodable, Equatable, Sendable {
    public let columns: [String]
    public let rows: [[String]]

    private enum CodingKeys: String, CodingKey { case columns, rows }
    private enum ColumnKeys: String, CodingKey { case label }

    private struct Column: Decodable {
        let label: String

        init(from decoder: Decoder) throws {
            label = try decoder.container(keyedBy: ColumnKeys.self).field(.label, "")
        }
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        columns = try c.field(.columns, [Column]()).map(\.label)
        rows = try c.field(.rows, [])
    }

    /// The label of column `index`, "" past the declared ones.
    public func column(_ index: Int) -> String { columns.indices.contains(index) ? columns[index] : "" }
}

public struct PluginText: Decodable, Equatable, Sendable {
    public let text: String
    public let tone: String
    public let mono: Bool

    private enum CodingKeys: String, CodingKey { case text, tone, mono }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        text = try c.field(.text, "")
        tone = try c.field(.tone, "")
        mono = try c.field(.mono, false)
    }
}

public struct PluginCondition: Decodable, Equatable, Sendable {
    public let type: String
    public let status: String
    public let reason: String
    public let message: String
    public let tone: String
    /// Unix ms, 0 when unknown.
    public let lastTransition: Int64

    private enum CodingKeys: String, CodingKey { case type, status, reason, message, tone, lastTransition }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        type = try c.field(.type, "")
        status = try c.field(.status, "")
        reason = try c.field(.reason, "")
        message = try c.field(.message, "")
        tone = try c.field(.tone, "")
        lastTransition = try c.field(.lastTransition, 0)
    }
}

public struct PluginFinding: Decodable, Equatable, Sendable {
    public let id: String
    public let severity: String
    public let title: String
    public let detail: String
    public let ref: PluginRef?
    public let opens: String

    private enum CodingKeys: String, CodingKey { case id, severity, title, detail, ref, opens }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        severity = try c.field(.severity, "")
        title = try c.field(.title, "")
        detail = try c.field(.detail, "")
        ref = try c.decodeIfPresent(PluginRef.self, forKey: .ref)
        opens = try c.field(.opens, "")
    }
}

/// A resource that could not be read, or a value that could not be evaluated: the rest still renders.
public struct PluginError: Decodable, Equatable, Sendable {
    public let resource: String
    public let `where`: String
    public let message: String

    public var subject: String { resource.isEmpty ? `where` : resource }

    /// "secrets: forbidden".
    public var line: String { [subject, message].filter { !$0.isEmpty }.joined(separator: ": ") }

    private enum CodingKeys: String, CodingKey { case resource, whereKey = "where", message }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        resource = try c.field(.resource, "")
        `where` = try c.field(.whereKey, "")
        message = try c.field(.message, "")
    }
}
