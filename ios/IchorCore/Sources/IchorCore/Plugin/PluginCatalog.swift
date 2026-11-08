import Foundation

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

    public init(
        id: String, name: String = "", icon: String = "", category: String = pluginCategoryDataServices,
        screen: String = "", active: Bool = true, detectedVia: String = "", summary: PluginSummary? = nil
    ) {
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

    /// The kind `ref` carries, else of its resource as plugin `pluginID` declares it; "" when
    /// neither says.
    public func kindOf(_ pluginID: String, _ ref: PluginRef) -> String {
        if !ref.kind.isEmpty { return ref.kind }

        return items.first { $0.id == pluginID }?.resources.first {
            $0.group == ref.group && $0.resource == ref.resource
        }?.kind ?? ""
    }

    /// The built-in plugins: the app's own, read-only in Settings.
    public var builtins: [PluginCatalogItem] { items.filter { !$0.user } }

    /// The app catalog ids of the Data services plugins (rook, mariadb...), for the inventory hints.
    public var dataServiceCatalogIDs: [String] {
        items.filter { $0.category == pluginCategoryDataServices }.map(\.catalogID).filter { !$0.isEmpty }
    }
}

public struct PluginCatalogItem: Decodable, Equatable, Sendable {
    public let id: String
    public let name: String
    public let category: String
    /// The app catalog id the inventory shows when the product runs; "" for none.
    public let catalogID: String
    public let resources: [PluginCatalogResource]
    /// Imported by the user (PluginUserImport), not a built-in.
    public let user: Bool

    private enum CodingKeys: String, CodingKey {
        case id, name, category, resources, user
        case catalogID = "catalogId"
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        name = try c.field(.name, "")
        category = try c.field(.category, "")
        catalogID = try c.field(.catalogID, "")
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
