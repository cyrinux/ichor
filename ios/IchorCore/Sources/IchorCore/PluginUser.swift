import Foundation

/// PluginUserList: the plugins the user imported, sorted by id. Mirrors go/ichorgo/plugin_store.go.
public struct PluginUserList: Decodable, Equatable, Sendable {
    public let items: [PluginUserItem]

    private enum CodingKeys: String, CodingKey { case items }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        items = try c.field(.items, [])
    }
}

/// An imported plugin as PluginUserImport and PluginUserList give it: what it reads and can do,
/// whether it is on, and `error`, why it cannot be turned on (a format this app does not read,
/// a file changed since the import), "" when it can.
public struct PluginUserItem: Decodable, Equatable, Identifiable, Sendable {
    public let id: String
    public let name: String
    public let website: String
    public let version: String
    public let resources: [PluginCatalogResource]
    public let actions: [PluginCatalogAction]
    public let enabled: Bool
    public let error: String
    public let importedAt: Int64
    /// From the check: an imported plugin has this id, and importing replaces it.
    public let replaces: Bool

    private enum CodingKeys: String, CodingKey { case id, name, website, version, resources, actions, enabled, error, importedAt, replaces }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        name = try c.field(.name, "")
        website = try c.field(.website, "")
        version = try c.field(.version, "")
        resources = try c.field(.resources, [])
        actions = try c.field(.actions, [])
        enabled = try c.field(.enabled, false)
        error = try c.field(.error, "")
        importedAt = try c.field(.importedAt, 0)
        replaces = try c.field(.replaces, false)
    }

    /// It can be turned on: it loads in this app.
    public var usable: Bool { error.isEmpty }

    /// The name to show: the plugin's, its id when it does not load.
    public var title: String { name.isEmpty ? id : name }

    /// "sleepinfos, secrets": what it reads, for its row.
    public var readsLine: String { resources.map(\.resource).joined(separator: ", ") }

    public var consent: PluginConsent {
        PluginConsent(
            id: id,
            name: title,
            website: website,
            resources: resources.map { r in
                let secret = r.group.isEmpty && r.resource == "secrets"
                return PluginConsentResource(name: r.group.isEmpty ? r.resource : "\(r.resource).\(r.group)",
                                             kind: r.kind, secret: secret, revealed: secret && r.reveal)
            },
            actions: actions.map { a in
                let needs = [a.rbac.joined(separator: ", "), a.rbacResource].filter { !$0.isEmpty }.joined(separator: " ")
                return PluginConsentAction(label: a.label.isEmpty ? a.id : a.label, verb: a.verb, needs: needs)
            },
            replaces: replaces
        )
    }
}

/// An action a plugin declares: its verb, and the Kubernetes `rbac` verbs it needs on `rbacResource`.
public struct PluginCatalogAction: Decodable, Equatable, Sendable {
    public let id: String
    public let label: String
    public let verb: String
    public let rbac: [String]
    public let rbacResource: String
    public let confirm: String

    private enum CodingKeys: String, CodingKey { case id, label, verb, rbac, rbacResource, confirm }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        label = try c.field(.label, "")
        verb = try c.field(.verb, "")
        rbac = try c.field(.rbac, [])
        rbacResource = try c.field(.rbacResource, "")
        confirm = try c.field(.confirm, "")
    }
}

/// A resource line of the consent sheet: "sleepinfos.kube-green.com" (or "secrets"), and
/// whether it is a Secret shown in clear.
public struct PluginConsentResource: Equatable, Hashable, Sendable {
    public let name: String
    public let kind: String
    public let secret: Bool
    public let revealed: Bool
}

/// An action line of the consent sheet: its label, and "patch sleepinfos" for what it needs.
public struct PluginConsentAction: Equatable, Hashable, Sendable {
    public let label: String
    public let verb: String
    public let needs: String
}

/// What the consent sheet shows before an imported plugin is turned on: what it reads, which
/// Secrets it shows in clear (flagged), and what it can change with which verbs. Same as
/// Android's PluginConsent.
public struct PluginConsent: Equatable, Sendable {
    public let id: String
    public let name: String
    public let website: String
    public let resources: [PluginConsentResource]
    public let actions: [PluginConsentAction]
    /// It replaces the imported plugin of the same id.
    public let replaces: Bool

    /// It shows at least one Secret's data in clear.
    public var revealsSecrets: Bool { resources.contains { $0.revealed } }

    /// It only reads: no action.
    public var readOnly: Bool { actions.isEmpty }
}

/// At most what an imported plugin file may be: the core refuses more.
public let pluginMaxBytes = 1 << 20
