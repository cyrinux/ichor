import Foundation

/// PluginIndexFetch: a published list of plugin files to pick from, and `url`, the index read.
/// Mirrors go/ichorgo/plugin_index.go.
public struct PluginIndex: Decodable, Equatable, Sendable {
    public let url: String
    public let generatedAt: String
    public let plugins: [PluginIndexItem]

    private enum CodingKeys: String, CodingKey { case url, generatedAt, plugins }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        url = try c.field(.url, "")
        generatedAt = try c.field(.generatedAt, "")
        plugins = try c.field(.plugins, [])
    }
}

/// A plugin file an index lists. `installed`: an imported plugin has its id. `builtin`: a built-in
/// has its id, so it cannot be imported. `icon` only names a bundled icon, and the core drops a
/// `website` that is not https.
public struct PluginIndexItem: Decodable, Equatable, Identifiable, Sendable {
    public let id: String
    public let name: String
    public let description: String
    public let version: String
    public let url: String
    public let sha256: String
    public let resources: [String]
    public let icon: String
    public let website: String
    public let maintainer: String
    public let installed: Bool
    public let builtin: Bool

    private enum CodingKeys: String, CodingKey {
        case id, name, description, version, url, sha256, resources, icon, website, maintainer, installed, builtin
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        name = try c.field(.name, "")
        description = try c.field(.description, "")
        version = try c.field(.version, "")
        url = try c.field(.url, "")
        sha256 = try c.field(.sha256, "")
        resources = try c.field(.resources, [])
        icon = try c.field(.icon, "")
        website = try c.field(.website, "")
        maintainer = try c.field(.maintainer, "")
        installed = try c.field(.installed, false)
        builtin = try c.field(.builtin, false)
    }

    public var title: String { name.isEmpty ? id : name }

    /// The tile: a bundled icon or a monogram, never a download.
    public var iconApp: InventoryApp { InventoryApp(id: id, name: title, icon: icon.nonEmpty) }
}
