import Foundation

// Mirrors go/ichorgo/supported_integrations.go.

/// The projects the app integrates with; `checked` once a cluster was asked which it runs.
public struct SupportedIntegrations: Decodable, Equatable, Sendable {
    public let checked: Bool
    public let items: [SupportedIntegration]

    public init(checked: Bool = false, items: [SupportedIntegration] = []) {
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

public struct SupportedIntegration: Decodable, Equatable, Sendable, Identifiable {
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
    /// How it was found, nil when not detected (or by a way this version does not know).
    public let via: SupportedIntegrationVia?
    /// The API version served for the first group, or the image tag of its pods; "" when unknown.
    public let version: String
    /// Where it runs, when found by its pods or Services.
    public let namespace: String

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        name = try c.field(.name, "")
        icon = try c.field(.icon, "")
        website = URL(string: try c.field(.website, ""))
        groups = try c.field(.groups, [])
        detected = try c.field(.detected, false)
        via = SupportedIntegrationVia(rawValue: try c.field(.via, ""))
        version = try c.field(.version, "")
        namespace = try c.field(.namespace, "")
    }

    /// The catalog app the integration is, for its icon.
    public var app: InventoryApp { InventoryApp(id: id, name: name, icon: icon.isEmpty ? nil : icon) }

    private enum CodingKeys: String, CodingKey { case id, name, icon, website, groups, detected, via, version, namespace }
}

/// How an integration was found on the cluster.
public enum SupportedIntegrationVia: String, Sendable {
    case api, pods, services
    /// The pods could not be listed: the inventory's hint.
    case inventory
}

/// The inventory's catalog ids, for KubeIntegrations: what has no API of its own is found by them.
public func supportedIntegrationHints(_ inventory: ClusterInventory) -> String {
    inventory.apps.filter(\.known).map(\.id).joined(separator: ",")
}
