import Foundation

// Mirrors go/ichorgo/node_files.go (NodeFiles, NodeFileRead).

/// One directory of a node: directories first, then by name, at most 2000 entries (`truncated`).
public struct NodeDirListing: Decodable, Equatable, Sendable {
    public let dir: String
    public let roots: [String]
    public let entries: [NodeFileEntry]
    public let truncated: Bool

    public init(dir: String, roots: [String] = [], entries: [NodeFileEntry] = [], truncated: Bool = false) {
        self.dir = dir
        self.roots = roots
        self.entries = entries
        self.truncated = truncated
    }

    private enum CodingKeys: String, CodingKey { case dir, roots, entries, truncated }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        dir = try c.field(.dir, "")
        roots = try c.field(.roots, [])
        entries = try c.field(.entries, [])
        truncated = try c.field(.truncated, false)
    }
}

/// An entry: `type` is file, dir or link; `modified` RFC 3339 or ""; `target` a link's target.
public struct NodeFileEntry: Decodable, Equatable, Hashable, Sendable, Identifiable {
    public let name: String
    public let path: String
    public let type: String
    public let size: Int64
    public let modified: String
    public let target: String

    public var id: String { path }
    public var isDir: Bool { type == "dir" }
    public var isLink: Bool { type == "link" }

    /// The modification time, nil when the node did not say.
    public var modifiedDate: Date? {
        modified.isEmpty ? nil : ISO8601DateFormatter().date(from: modified)
    }

    public init(
        name: String, path: String, type: String = "file", size: Int64 = 0, modified: String = "", target: String = ""
    ) {
        self.name = name
        self.path = path
        self.type = type
        self.size = size
        self.modified = modified
        self.target = target
    }

    private enum CodingKeys: String, CodingKey { case name, path, type, size, modified, target }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        path = try c.field(.path, "")
        type = try c.field(.type, "file")
        size = try c.field(.size, 0)
        modified = try c.field(.modified, "")
        target = try c.field(.target, "")
    }
}

/// A file's text up to 512 KiB (`truncated` past it), none when `binary`; secrets `redacted`.
public struct NodeFileText: Decodable, Equatable, Sendable {
    public let path: String
    public let size: Int64
    public let text: String
    public let truncated: Bool
    public let binary: Bool
    public let redacted: Bool

    public init(
        path: String, size: Int64 = 0, text: String = "", truncated: Bool = false, binary: Bool = false,
        redacted: Bool = false
    ) {
        self.path = path
        self.size = size
        self.text = text
        self.truncated = truncated
        self.binary = binary
        self.redacted = redacted
    }

    private enum CodingKeys: String, CodingKey { case path, size, text, truncated, binary, redacted }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        path = try c.field(.path, "")
        size = try c.field(.size, 0)
        text = try c.field(.text, "")
        truncated = try c.field(.truncated, false)
        binary = try c.field(.binary, false)
        redacted = try c.field(.redacted, false)
    }
}

/// One step of the path bar: `name` ("/" for the root) opens `path`.
public struct NodePathCrumb: Equatable, Hashable, Sendable {
    public let name: String
    public let path: String

    public init(name: String, path: String) {
        self.name = name
        self.path = path
    }
}

public enum NodePaths {
    /// The steps from "/" to `dir`: "/var/log" → "/", "var", "log".
    public static func crumbs(_ dir: String) -> [NodePathCrumb] {
        let parts = dir.split(separator: "/").map(String.init)
        return [NodePathCrumb(name: "/", path: "/")]
            + parts.indices.map { i in
                NodePathCrumb(name: parts[i], path: "/" + parts[0...i].joined(separator: "/"))
            }
    }

    /// A typed path the core accepts: absolute, without ".."; nil when it would be refused.
    public static func typed(_ input: String) -> String? {
        let path = input.trimmingCharacters(in: .whitespacesAndNewlines)
        guard path.hasPrefix("/"), !path.split(separator: "/").contains("..") else { return nil }
        return path
    }
}
