import Foundation

// Mirrors go/ichorgo/talos_patch.go.

/// One document of a Talos patch file: `kind` is v1alpha1 for machine/cluster sections.
public struct TalosPatchDoc: Decodable, Equatable, Sendable {
    public let kind: String
    public let name: String
    public let hostname: String

    private enum CodingKeys: String, CodingKey { case kind, name, hostname }

    public init(kind: String, name: String = "", hostname: String = "") {
        self.kind = kind
        self.name = name
        self.hostname = hostname
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.field(.kind, "")
        name = try c.field(.name, "")
        hostname = try c.field(.hostname, "")
    }
}

/// What a repository file is to Talos: `talos` when it is a patch (or a whole config); it can
/// be tried on a node when `tryable`, else `note` says why (a secret, a whole config, a talenv
/// placeholder).
public struct TalosPatchInfo: Decodable, Equatable, Sendable {
    public let talos: Bool
    /// patch, documents, jsonpatch or config.
    public let kind: String
    public let documents: [TalosPatchDoc]
    public let touchesSecrets: Bool
    public let secret: String
    public let tryable: Bool
    public let note: String

    private enum CodingKeys: String, CodingKey { case talos, kind, documents, touchesSecrets, secret, tryable, note }

    public init(talos: Bool, kind: String = "", documents: [TalosPatchDoc] = [], tryable: Bool = false, note: String = "") {
        self.talos = talos
        self.kind = kind
        self.documents = documents
        touchesSecrets = false
        secret = ""
        self.tryable = tryable
        self.note = note
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        talos = try c.field(.talos, false)
        kind = try c.field(.kind, "")
        documents = try c.field(.documents, [])
        touchesSecrets = try c.field(.touchesSecrets, false)
        secret = try c.field(.secret, "")
        tryable = try c.field(.tryable, false)
        note = try c.field(.note, "")
    }

    /// The hostnames the file sets: the node it is meant for.
    public var hostnames: [String] {
        var seen: [String] = []
        for doc in documents where !doc.hostname.isEmpty && !seen.contains(doc.hostname) { seen.append(doc.hostname) }
        return seen
    }

    /// The node it is meant for among `nodes`, by the hostname it sets (an FQDN matching its
    /// short name too), nil when it names none of them.
    public func node(in nodes: [NodeOverview]) -> NodeOverview? {
        let wanted = hostnames
        guard !wanted.isEmpty else { return nil }
        func short(_ h: String) -> String { String(h.split(separator: ".").first ?? "").lowercased() }
        return nodes.first { n in wanted.contains { $0.caseInsensitiveCompare(n.hostname) == .orderedSame } }
            ?? nodes.first { n in wanted.contains { short($0) == short(n.hostname) } }
    }
}

/// A node's config with the patch applied, redacted: the editor's draft.
public struct TalosPatchDraft: Decodable, Equatable, Sendable {
    public let draft: String
    public let same: Bool
    public let needsReboot: Bool

    private enum CodingKeys: String, CodingKey { case draft, same, needsReboot }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        draft = try c.field(.draft, "")
        same = try c.field(.same, false)
        needsReboot = try c.field(.needsReboot, false)
    }
}

/// A patch file to try on a node, carried to the machine config screen: its `content` and where
/// it comes from (`from`, "path @ commit"), shown above the draft.
public struct PatchRequest: Hashable, Sendable {
    public let content: String
    public let from: String

    public init(content: String, from: String) {
        self.content = content
        self.from = from
    }

    /// A file the size of a patch, in UTF-8 bytes: larger ones are not offered.
    public static let maxBytes = 16 * 1024
}
