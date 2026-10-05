import Foundation

// Mirrors go/ichorgo/integrations.go.

/// The projects the app integrates with; `checked` once a cluster was asked which it runs.
public struct Integrations: Decodable, Equatable, Sendable {
    public let checked: Bool
    public let items: [Integration]

    public init(checked: Bool = false, items: [Integration] = []) {
        self.checked = checked
        self.items = items
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        checked = try c.field(.checked, false)
        items = try c.field(.items, [])
    }

    private enum CodingKeys: String, CodingKey { case checked, items }
}

public struct Integration: Decodable, Equatable, Sendable, Identifiable {
    /// Catalog app id.
    public let id: String
    /// A proper noun: never translated.
    public let name: String
    /// Bundled icon name, "" when none.
    public let icon: String
    public let website: URL?
    /// The API groups the app reads; empty when found by its pods (Garage).
    public let groups: [String]
    public let detected: Bool
    /// API version the cluster serves for the first group; "" when unknown.
    public let version: String

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        name = try c.field(.name, "")
        icon = try c.field(.icon, "")
        website = URL(string: try c.field(.website, ""))
        groups = try c.field(.groups, [])
        detected = try c.field(.detected, false)
        version = try c.field(.version, "")
    }

    /// The catalog app the integration is, for its icon.
    public var app: InventoryApp { InventoryApp(id: id, name: name, icon: icon.isEmpty ? nil : icon) }

    private enum CodingKeys: String, CodingKey { case id, name, icon, website, groups, detected, version }
}

/// The inventory's catalog ids, for KubeIntegrations: what has no API of its own is found by them.
public func integrationHints(_ inventory: ClusterInventory) -> String {
    inventory.apps.filter(\.known).map(\.id).joined(separator: ",")
}
