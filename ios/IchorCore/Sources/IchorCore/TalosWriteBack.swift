import Foundation

// Writing a kept machine config change back to the cluster repository, as the Go core answers it
// (go/ichorgo/talos_writeback*.go). Same records as Android (model/TalosWriteBack.kt).

/// A config value as one line of text: a string bare, a list or a mapping in JSON's shape.
public indirect enum ConfigValueText: Decodable, Hashable, Sendable {
    case text(String)
    case list([ConfigValueText])
    case map([String: ConfigValueText])
    case null

    public init(from decoder: Decoder) throws {
        let c = try decoder.singleValueContainer()
        if c.decodeNil() {
            self = .null
        } else if let s = try? c.decode(String.self) {
            self = .text(s)
        } else if let b = try? c.decode(Bool.self) {
            self = .text(b ? "true" : "false")
        } else if let i = try? c.decode(Int.self) {
            self = .text(String(i))
        } else if let d = try? c.decode(Double.self) {
            self = .text(String(d))
        } else if let l = try? c.decode([ConfigValueText].self) {
            self = .list(l)
        } else {
            self = .map(try c.decode([String: ConfigValueText].self))
        }
    }

    /// One line: "/dev/sda", ["a","b"], {"max-pods":"150"}; "—" for none.
    public var display: String {
        switch self {
        case .text(let s): s
        case .null: "—"
        case .list(let l): "[" + l.map(\.quoted).joined(separator: ",") + "]"
        case .map(let m): "{" + m.keys.sorted().map { "\"\($0)\":\(m[$0]?.quoted ?? "null")" }.joined(separator: ",") + "}"
        }
    }

    private var quoted: String {
        if case .text(let s) = self { return "\"\(s)\"" }
        return display
    }
}

/// One value a kept change sets: `kind` the document ("v1alpha1", "HostnameConfig"), `path`
/// dotted, `from` nil for a new value, `to` nil for a removed one (not written back).
public struct TalosConfigChange: Decodable, Hashable, Sendable, Identifiable {
    public let doc: Int
    public let kind: String
    public let path: String
    public let from: ConfigValueText?
    public let to: ConfigValueText?

    public var id: String { "\(doc):\(kind):\(path)" }
    public var removed: Bool { to == nil || to == .null }

    enum CodingKeys: String, CodingKey { case doc, kind, path, from, to }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        doc = try c.field(.doc, 0)
        kind = try c.field(.kind, "")
        path = try c.field(.path, "")
        from = try c.decodeIfPresent(ConfigValueText.self, forKey: .from)
        to = try c.decodeIfPresent(ConfigValueText.self, forKey: .to)
    }
}

/// The patch of a kept change, its values, whether one is a secret's, what it leaves out.
public struct TalosConfigPatch: Decodable, Equatable, Sendable {
    public let patch: String
    public let changes: [TalosConfigChange]
    public let touchesSecrets: Bool
    public let notes: [String]

    /// The first value written: the pull request's title names it.
    public var firstPath: String { changes.first { !$0.removed }?.path ?? "" }

    enum CodingKeys: String, CodingKey { case patch, changes, touchesSecrets, notes }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        patch = try c.field(.patch, "")
        changes = try c.field(.changes, [])
        touchesSecrets = try c.field(.touchesSecrets, false)
        notes = try c.field(.notes, [])
    }
}

/// Where a write-back goes: the node's `file` (a new one when `create`), the `patch` left to
/// write, the values `skipped` because a shared patch sets them.
public struct TalosWriteBackPlan: Decodable, Equatable, Sendable {
    public let sha: String
    public let file: String
    public let create: Bool
    public let patch: String
    public let skipped: [TalosRepoSkip]

    public init(sha: String = "", file: String, create: Bool = false, patch: String, skipped: [TalosRepoSkip] = []) {
        self.sha = sha
        self.file = file
        self.create = create
        self.patch = patch
        self.skipped = skipped
    }

    enum CodingKeys: String, CodingKey { case sha, file, create, patch, skipped }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        sha = try c.field(.sha, "")
        file = try c.field(.file, "")
        create = try c.field(.create, false)
        patch = try c.field(.patch, "")
        skipped = try c.field(.skipped, [])
    }
}
