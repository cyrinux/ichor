import Foundation

/// A parameter of a config snippet (Go ConfigSnippets); `hint` is an example value (verbatim).
public struct SnippetParam: Decodable, Equatable, Hashable, Sendable {
    public let name: String
    /// text, list (one per line), int or bool.
    public let type: String
    public let hint: String

    private enum CodingKeys: String, CodingKey { case name, type, hint }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        type = try c.field(.type, "text")
        hint = try c.field(.hint, "")
    }
}

/// A ready-made machine config change; the app translates its `id` and `group`.
public struct ConfigSnippet: Decodable, Equatable, Identifiable, Sendable {
    public let id: String
    public let group: String
    public let params: [SnippetParam]
    /// Takes effect at the next boot only: it cannot be tried.
    public let staged: Bool

    private enum CodingKeys: String, CodingKey { case id, group, params, staged }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        group = try c.field(.group, "")
        params = try c.field(.params, [])
        staged = try c.field(.staged, false)
    }
}

public struct ConfigSnippetList: Decodable, Equatable, Sendable {
    public let snippets: [ConfigSnippet]

    private enum CodingKeys: String, CodingKey { case snippets }

    public init(from decoder: Decoder) throws {
        snippets = try decoder.container(keyedBy: CodingKeys.self).field(.snippets, [])
    }

    /// The snippets of each group, groups in the order the core lists them.
    public var byGroup: [(group: String, snippets: [ConfigSnippet])] {
        var order: [String] = []
        var groups: [String: [ConfigSnippet]] = [:]
        for s in snippets {
            if groups[s.group] == nil { order.append(s.group) }
            groups[s.group, default: []].append(s)
        }
        return order.map { ($0, groups[$0] ?? []) }
    }
}

/// The edits a snippet or a drift fix makes to the draft; `reason` says why when not `fixable`.
public struct SnippetEdits: Decodable, Equatable, Sendable {
    public let fixable: Bool
    public let reason: String
    public let edits: [ConfigEdit]
    public let staged: Bool

    private enum CodingKeys: String, CodingKey { case fixable, reason, edits, staged }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        fixable = try c.field(.fixable, false)
        reason = try c.field(.reason, "")
        edits = try c.field(.edits, [])
        staged = try c.field(.staged, false)
    }
}

/// A drift difference to undo with a config change: the node gets the reference's value `before`.
public struct DriftFix: Equatable, Hashable, Sendable {
    public let key, before, after: String

    public init(key: String, before: String, after: String) {
        self.key = key
        self.before = before
        self.after = after
    }
}

extension DriftChange {
    /// The fix of this difference, or nil when no config change can undo it (Go DriftFixEdits).
    public var fix: DriftFix? {
        guard ["dns", "ntp", "mtu"].contains(key), !before.trimmingCharacters(in: .whitespaces).isEmpty else { return nil }
        return DriftFix(key: key, before: before, after: after)
    }
}
