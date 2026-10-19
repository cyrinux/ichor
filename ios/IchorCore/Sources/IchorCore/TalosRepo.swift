import Foundation

// Talos from Git, as the Go core answers it (go/ichorgo/talos_repo.go): the files of the cluster
// repository that configure a node, and the node's live config with them applied. Same records
// as Android (model/TalosRepo.kt).

/// What a repository file is to a node: its own patch, a shared one, talhelper's talconfig.yaml, or secrets.
public enum TalosRepoFileKind: String, Decodable, Sendable {
    case node, shared, talconfig, secrets

    public init(from decoder: Decoder) throws {
        self = Self(rawValue: try decoder.singleValueContainer().decode(String.self)) ?? .shared
    }
}

/// One file of TalosRepoFiles: `why` the rule that matched; `patch` whether it can be applied
/// (secrets files and talconfig.yaml itself cannot). An inline talconfig patch is
/// "talconfig.yaml#L<line>", `line` its line.
public struct TalosRepoFile: Decodable, Hashable, Sendable, Identifiable {
    public let path: String
    public let kind: TalosRepoFileKind
    public let why: String
    public let line: Int
    public let patch: Bool

    public var id: String { path }
    /// The file itself, without an inline patch's line mark.
    public var file: String { path.components(separatedBy: "#L").first ?? path }

    public init(path: String, kind: TalosRepoFileKind, why: String = "", line: Int = 0, patch: Bool = false) {
        self.path = path
        self.kind = kind
        self.why = why
        self.line = line
        self.patch = patch
    }

    enum CodingKeys: String, CodingKey { case path, kind, why, line, patch }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        path = try c.field(.path, "")
        kind = try c.field(.kind, .shared)
        why = try c.field(.why, "")
        line = try c.field(.line, 0)
        patch = try c.field(.patch, false)
    }
}

/// A node's files in the cluster repository at `sha`, laid out for `generator` (talhelper, talosctl, unknown).
public struct TalosRepoFiles: Decodable, Equatable, Sendable {
    public let sha: String
    public let generator: String
    public let files: [TalosRepoFile]
    public let note: String

    /// What Compare applies first: the node's own patch and the shared ones.
    public var defaultSelection: Set<String> { Set(files.filter(\.patch).map(\.path)) }

    /// The paths of files picked in selected, in the order Compare applies them.
    public func ordered(_ selected: Set<String>) -> [String] {
        files.filter { $0.patch && selected.contains($0.path) }.map(\.path)
    }

    public init(sha: String = "", generator: String = "", files: [TalosRepoFile] = [], note: String = "") {
        self.sha = sha
        self.generator = generator
        self.files = files
        self.note = note
    }

    enum CodingKeys: String, CodingKey { case sha, generator, files, note }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        sha = try c.field(.sha, "")
        generator = try c.field(.generator, "")
        files = try c.field(.files, [])
        note = try c.field(.note, "")
    }
}

public struct TalosRepoSkip: Decodable, Hashable, Sendable {
    public let path: String
    public let why: String

    enum CodingKeys: String, CodingKey { case path, why }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        path = try c.field(.path, "")
        why = try c.field(.why, "")
    }
}

/// The node's config with the repository's patches applied, against the node. `draft` is that
/// config, redacted: the editor's draft for "Edit from this diff".
public struct TalosRepoCompare: Decodable, Equatable, Sendable {
    public let sha: String
    public let files: [String]
    public let diff: FluxDiff
    public let same: Bool
    public let needsReboot: Bool
    public let skipped: [TalosRepoSkip]
    public let draft: String

    enum CodingKeys: String, CodingKey { case sha, files, diff, same, needsReboot, skipped, draft }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        sha = try c.field(.sha, "")
        files = try c.field(.files, [])
        diff = try c.field(.diff, FluxDiff())
        same = try c.field(.same, false)
        needsReboot = try c.field(.needsReboot, false)
        skipped = try c.field(.skipped, [])
        draft = try c.field(.draft, "")
    }
}

/// The node's role in a config ("controlplane", "worker"), from its machine.type; "" when it says none.
public func configRole(_ configYAML: String) -> String {
    var inMachine = false
    for line in configYAML.split(separator: "\n", omittingEmptySubsequences: false) {
        if let first = line.first, !first.isWhitespace { inMachine = line.trimmingCharacters(in: .whitespaces) == "machine:" }
        if inMachine, line.hasPrefix("  type:") {
            let type = line.dropFirst("  type:".count).trimmingCharacters(in: .whitespaces).trimmingCharacters(in: CharacterSet(charactersIn: "\"'"))
            return type == "init" ? "controlplane" : type
        }
    }
    return ""
}
