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

    public var serviceHealth: ServiceHealth { pluginHealth(health) }

    /// The cards worth drawing: those with rows, or with a text for none.
    public var shownCards: [PluginCard] { cards.filter { !$0.rows.isEmpty || !$0.empty.isEmpty } }

    /// The unreadable parts as lines: "secrets: forbidden".
    public var errorLines: [String] { errors.map(\.line) }

    private enum CodingKeys: String, CodingKey { case plugin, active, detectedVia, apiVersion, health, partial, summary, cards, findings, errors }

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

    /// The rows to draw, with their index (for stable ids): one group per namespace, in the order
    /// they first appear, when `groupsByNamespace`; else one group titled "".
    public var rowGroups: [PluginRowGroup] {
        let indexed = rows.enumerated().map { PluginIndexedRow(index: $0.offset, row: $0.element) }
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
    public let row: PluginRow
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

    public init(group: String = "", version: String = "", resource: String = "", namespace: String = "", name: String = "") {
        self.group = group
        self.version = version
        self.resource = resource
        self.namespace = namespace
        self.name = name
    }

    private enum CodingKeys: String, CodingKey { case group, version, resource, namespace, name }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        group = try c.field(.group, "")
        version = try c.field(.version, "")
        resource = try c.field(.resource, "")
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
    }

    public var key: String { "\(group)/\(resource)/\(namespace)/\(name)" }

    /// "demo-lab/weekend", or the name of a cluster-scoped object.
    public var label: String { [namespace, name].filter { !$0.isEmpty }.joined(separator: "/") }

    /// The object screen's resource; `kind` comes from the plugin catalog ("" when unknown: only
    /// the title misses it).
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

    /// The key of its expanded state, unique within the tree.
    public func expandKey(card: String, index: Int) -> String {
        "\(card)|\(ref?.key ?? "\(index):\(title)")"
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

/// A declared action's button.
public struct PluginActionItem: Decodable, Equatable, Sendable {
    public let id: String
    public let label: String
    public let enabled: Bool
    /// none, plain or typedName.
    public let confirm: String
    public let disabledReason: String
    /// The can-I answer; nil until the actions step sets it.
    public let allowed: Bool?
    public let params: [PluginParam]

    /// Whether the button may be pressed, as far as the tree says.
    public var usable: Bool { enabled && allowed != false }

    /// Why the button is off. Running a plugin action needs the actions step of the Go core
    /// (KubePluginAction), not in this build: every button stays off, with the plugin's reason
    /// when it gives one.
    public var note: PluginActionNote {
        if !enabled && !disabledReason.isEmpty { return .reason(disabledReason) }
        if allowed == false { return .notAllowed }
        return .unavailable
    }

    private enum CodingKeys: String, CodingKey { case id, label, enabled, confirm, disabledReason, allowed, params }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        label = try c.field(.label, "")
        enabled = try c.field(.enabled, false)
        confirm = try c.field(.confirm, "")
        disabledReason = try c.field(.disabledReason, "")
        allowed = try c.decodeIfPresent(Bool.self, forKey: .allowed)
        params = try c.field(.params, [])
    }
}

/// What an action button says under it while it is off.
public enum PluginActionNote: Equatable, Sendable {
    /// The plugin's own reason ("not asleep").
    case reason(String)
    /// The credentials may not do it.
    case notAllowed
    /// This build cannot run plugin actions yet.
    case unavailable
}

public struct PluginParam: Decodable, Equatable, Sendable {
    public let name: String
    public let label: String
    /// string, int, bool or enum.
    public let type: String
    public let values: [String]

    private enum CodingKeys: String, CodingKey { case name, label, type, values }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        label = try c.field(.label, "")
        type = try c.field(.type, "")
        values = try c.field(.values, [])
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

/// KubePluginDetect: every plugin, active or not.
public struct PluginDetect: Decodable, Equatable, Sendable {
    public let items: [PluginDetectItem]

    public init(items: [PluginDetectItem] = []) {
        self.items = items
    }

    private enum CodingKeys: String, CodingKey { case items }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        items = try c.field(.items, [])
    }

    public var active: [PluginDetectItem] { items.filter(\.active) }

    /// The active plugins of Data services: a tile in its card, a tab in its screen (unless a
    /// native screen stands in).
    public var dataServicePlugins: [PluginDetectItem] { active.filter { $0.category == pluginCategoryDataServices } }

    /// The active plugins that open from the Kubernetes home's tools instead.
    public var homeTiles: [PluginDetectItem] { active.filter { $0.category != pluginCategoryDataServices } }

    /// The Data services tabs of plugins: those without a native screen standing in.
    public var dataServiceTabs: [PluginDetectItem] { dataServicePlugins.filter { $0.nativeScreen == nil } }
}

public struct PluginDetectItem: Decodable, Equatable, Sendable, Identifiable {
    public let id: String
    public let name: String
    public let icon: String
    public let category: String
    public let screen: String
    public let active: Bool
    public let detectedVia: String
    /// The tile of an active plugin; nil otherwise.
    public let summary: PluginSummary?

    public init(id: String, name: String = "", icon: String = "", category: String = pluginCategoryDataServices,
                screen: String = "", active: Bool = true, detectedVia: String = "", summary: PluginSummary? = nil) {
        self.id = id
        self.name = name
        self.icon = icon
        self.category = category
        self.screen = screen
        self.active = active
        self.detectedVia = detectedVia
        self.summary = summary
    }

    private enum CodingKeys: String, CodingKey { case id, name, icon, category, screen, active, detectedVia, summary }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        name = try c.field(.name, "")
        icon = try c.field(.icon, "")
        category = try c.field(.category, pluginCategoryDataServices)
        screen = try c.field(.screen, "")
        active = try c.field(.active, false)
        detectedVia = try c.field(.detectedVia, "")
        summary = try c.decodeIfPresent(PluginSummary.self, forKey: .summary)
    }

    /// The app screen `screen: native:<route>` names, nil for the generic screen or a name
    /// outside the allow-list.
    public var nativeScreen: PluginNativeScreen? { PluginNativeScreen(screen: screen) }

    /// The family of the Kubernetes home's tools its tile joins.
    public var toolFamily: PluginToolFamily { PluginToolFamily(category: category) }

    /// The data service a catalog icon names ("kube-green"), for its symbol; nil otherwise.
    public var dataServiceKind: DataServiceKind? { DataServiceKind(rawValue: icon.isEmpty ? id : icon) }
}

/// PluginCatalog: what each installed plugin (the built-ins, then the enabled imported ones)
/// reads, for the kinds of the objects its rows open and the Settings list.
public struct PluginCatalog: Decodable, Equatable, Sendable {
    public let items: [PluginCatalogItem]

    public init(items: [PluginCatalogItem] = []) {
        self.items = items
    }

    private enum CodingKeys: String, CodingKey { case items }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        items = try c.field(.items, [])
    }

    /// The kind of `ref`'s resource as plugin `pluginID` declares it; "" when it does not.
    public func kindOf(_ pluginID: String, _ ref: PluginRef) -> String {
        items.first { $0.id == pluginID }?.resources.first { $0.group == ref.group && $0.resource == ref.resource }?.kind ?? ""
    }

    /// The built-in plugins: the app's own, read-only in Settings.
    public var builtins: [PluginCatalogItem] { items.filter { !$0.user } }
}

public struct PluginCatalogItem: Decodable, Equatable, Sendable {
    public let id: String
    public let name: String
    public let category: String
    public let resources: [PluginCatalogResource]
    /// Imported by the user (PluginUserImport), not a built-in.
    public let user: Bool

    private enum CodingKeys: String, CodingKey { case id, name, category, resources, user }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        name = try c.field(.name, "")
        category = try c.field(.category, "")
        resources = try c.field(.resources, [])
        user = try c.field(.user, false)
    }
}

public struct PluginCatalogResource: Decodable, Equatable, Sendable {
    public let group: String
    public let version: String
    public let resource: String
    public let kind: String
    /// A Secret's data is shown in clear instead of masked.
    public let reveal: Bool

    private enum CodingKeys: String, CodingKey { case group, version, resource, kind, reveal }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        group = try c.field(.group, "")
        version = try c.field(.version, "")
        resource = try c.field(.resource, "")
        kind = try c.field(.kind, "")
        reveal = try c.field(.reveal, false)
    }
}

/// The app screens a plugin may name with `screen: native:<route>`: only these, so a plugin
/// (one day a user's) can never send the app anywhere else. A data service's catalog id
/// ("longhorn") opens its Data services tab. Same names as Android's PluginRoutes.kt.
public enum PluginNativeScreen: Hashable, Sendable {
    case argoCD, flux, argoRollouts, policies, netpol, autoscaling, vms, crossplane, tailscale
    case clusterLifecycle, loadBalancing, storage, services, jobs, helm
    case dataService(DataServiceKind)

    private static let named: [String: PluginNativeScreen] = [
        "argocd": .argoCD, "flux": .flux, "argo-rollouts": .argoRollouts, "policies": .policies,
        "netpol": .netpol, "autoscaling": .autoscaling, "vms": .vms, "crossplane": .crossplane,
        "tailscale": .tailscale, "cluster-lifecycle": .clusterLifecycle, "load-balancing": .loadBalancing,
        "storage": .storage, "services": .services, "jobs": .jobs, "helm": .helm,
    ]

    /// The screen `screen` names ("native:argocd"); nil for "", the generic screen, or a name
    /// this build does not know.
    public init?(screen: String) {
        guard let name = nativePluginScreen(screen) else { return nil }
        if let known = Self.named[name] {
            self = known
        } else if let kind = DataServiceKind(rawValue: name) {
            self = .dataService(kind)
        } else {
            return nil
        }
    }
}

private let nativePrefix = "native:"

/// "argocd" for "native:argocd"; nil for "" or anything else.
public func nativePluginScreen(_ screen: String) -> String? {
    let trimmed = screen.trimmingCharacters(in: .whitespaces)
    guard trimmed.hasPrefix(nativePrefix) else { return nil }
    let name = trimmed.dropFirst(nativePrefix.count).trimmingCharacters(in: .whitespaces)
    return name.isEmpty ? nil : name
}

/// The family of the Kubernetes home's tools a plugin of another category joins; storage gets
/// none: data services plugins are tabs of the Data services screen instead.
public enum PluginToolFamily: Sendable, CaseIterable {
    case workloads, networking, observe, browse

    public init(category: String) {
        switch category {
        case "workloads", "compute": self = .workloads
        case "networking": self = .networking
        case "policies", "security": self = .observe
        default: self = .browse
        }
    }
}

/// A plugin tone as a health level: ok, idle, warning and critical as such; info reads calm
/// (idle), unknown and anything else unknown.
public func pluginHealth(_ tone: String) -> ServiceHealth {
    switch tone {
    case "ok": .ok
    case "idle", "info": .idle
    case "warning": .warning
    case "critical": .critical
    default: .unknown
    }
}

/// A tab of the Data services hub: a native system, or a CRD plugin drawn from its render tree.
public enum DataServicesTab: Hashable, Sendable, Identifiable {
    case native(DataServiceKind)
    case plugin(id: String, name: String)

    /// "longhorn", "plugin:kube-green".
    public var id: String {
        switch self {
        case .native(let kind): kind.rawValue
        case .plugin(let id, _): "plugin:\(id)"
        }
    }

    /// The native systems first, then one tab per plugin without a native screen.
    public static func tabs(detected: [DataServiceKind], plugins: PluginDetect?) -> [DataServicesTab] {
        detected.map { .native($0) } + (plugins?.dataServiceTabs ?? []).map { .plugin(id: $0.id, name: $0.name) }
    }
}

/// When a plugin tab reads again: a new `scope` (the cluster and its config generation) starts
/// over, so another cluster's rows never show; a new `bump` (a refresh, a change made from the
/// tab) reads in place, keeping the rows.
public struct PluginReload: Hashable, Sendable {
    public let scope: String
    public let bump: Int

    public init(scope: String, bump: Int) {
        self.scope = scope
        self.bump = bump
    }

    public enum Change: Equatable, Sendable { case none, reset, refresh }

    /// What moving from `previous` (nil: nothing loaded yet) to self asks for.
    public func change(from previous: PluginReload?) -> Change {
        guard let previous, previous.scope == scope else { return .reset }
        return previous.bump == bump ? .none : .refresh
    }
}

/// A line of the Data services card: a native system, or a CRD plugin with its summary tile.
public enum DataServicesCardLine: Equatable, Sendable, Identifiable {
    case native(DataServiceKind, health: ServiceHealth?)
    case plugin(PluginDetectItem)

    public var id: String {
        switch self {
        case .native(let kind, _): kind.rawValue
        case .plugin(let item): "plugin:\(item.id)"
        }
    }

    /// Nil when nothing says (a system not read yet, a plugin without its tile).
    public var health: ServiceHealth? {
        switch self {
        case .native(_, let health): health
        case .plugin(let item): item.summary?.serviceHealth
        }
    }

    /// Worst first (critical, warning, then the calm levels; no health counts as unknown), as on
    /// Android; equal ones keep their order, the native systems before the plugins.
    public static func lines(natives: [DataServiceKind], summary: (DataServiceKind) -> ServiceSummary?,
                             plugins: [PluginDetectItem]) -> [DataServicesCardLine] {
        let all = natives.map { DataServicesCardLine.native($0, health: summary($0)?.health) } + plugins.map { .plugin($0) }
        return all.enumerated()
            .sorted { a, b in
                let ha = a.element.health ?? .unknown
                let hb = b.element.health ?? .unknown
                return ha == hb ? a.offset < b.offset : ha < hb
            }
            .map(\.element)
    }

    /// "N need a look": what each native system counts, plus one per plugin whose tile needs
    /// attention (a tile has no count of its own).
    public static func attention(natives: [DataServiceKind], summary: (DataServiceKind) -> ServiceSummary?,
                                 plugins: [PluginDetectItem]) -> Int {
        natives.compactMap { summary($0)?.attention }.reduce(0, +)
            + plugins.filter { $0.summary?.serviceHealth.needsAttention == true }.count
    }
}

public extension DataServices {
    /// The card's lines: these systems and `plugins`, worst first.
    func cardLines(plugins: [PluginDetectItem]) -> [DataServicesCardLine] {
        DataServicesCardLine.lines(natives: detected, summary: { summary($0) }, plugins: plugins)
    }

    /// The card's "N need a look", plugins included.
    func attention(plugins: [PluginDetectItem]) -> Int {
        DataServicesCardLine.attention(natives: detected, summary: { summary($0) }, plugins: plugins)
    }
}
