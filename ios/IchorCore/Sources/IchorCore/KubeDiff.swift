import Foundation

// Mirrors go/ichorgo/kube_diff.go and kube_flux_diff.go: what a GitOps tool would change in the
// cluster, object by object.

/// What reconciling a Flux object now would change.
public struct FluxDiff: Decodable, Equatable, Sendable {
    public let kind: String
    public let namespace: String
    public let name: String
    /// The source revision built.
    public let revision: String
    /// The revision the controller last applied.
    public let applied: String
    /// Sorted by the Go core: what needs a look first.
    public let resources: [KubeDiffResource]
    public let warnings: [String]

    public init(kind: String = "Kustomization", namespace: String = "", name: String = "", revision: String = "",
                applied: String = "", resources: [KubeDiffResource] = [], warnings: [String] = []) {
        self.kind = kind
        self.namespace = namespace
        self.name = name
        self.revision = revision
        self.applied = applied
        self.resources = resources
        self.warnings = warnings
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.field(.kind, "")
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        revision = try c.field(.revision, "")
        applied = try c.field(.applied, "")
        resources = try c.field(.resources, [])
        warnings = try c.field(.warnings, [])
    }

    private enum CodingKeys: String, CodingKey { case kind, namespace, name, revision, applied, resources, warnings }

    /// Counts by change, in `DiffChange` order, the empty ones left out.
    public var counts: [DiffCount] {
        DiffChange.allCases.compactMap { change in
            let n = resources.filter { $0.change == change }.count
            return n > 0 ? DiffCount(change: change, count: n) : nil
        }
    }

    /// The objects worth a look; the unchanged ones are shown folded.
    public var changed: [KubeDiffResource] { resources.filter { $0.change != .unchanged } }
    public var unchanged: [KubeDiffResource] { resources.filter { $0.change == .unchanged } }

    /// Nothing would change: every object is unchanged, ignored or not compared (encrypted).
    public var inSync: Bool { !resources.contains { $0.change.isChange || $0.change == .error } }

    /// A new source revision is built, not the one last applied.
    public var newRevision: Bool { !revision.isEmpty && !applied.isEmpty && revision != applied }
}

/// How many objects a diff has with one kind of change.
public struct DiffCount: Equatable, Hashable, Sendable {
    public let change: DiffChange
    public let count: Int

    public init(change: DiffChange, count: Int) {
        self.change = change
        self.count = count
    }
}

/// One object of a diff.
public struct KubeDiffResource: Decodable, Equatable, Identifiable, Sendable {
    public let group: String
    public let version: String
    public let kind: String
    public let namespace: String
    public let name: String
    public let change: DiffChange
    /// A unified diff from live to wanted, "" when unchanged or not compared.
    public let diff: String
    /// The diff was cut at 64 KB.
    public let truncated: Bool
    /// Why the API server refused the dry run, for `.error`.
    public let error: String

    public var id: String { "\(group)/\(kind)/\(namespace)/\(name)" }

    public init(group: String = "", version: String = "v1", kind: String, namespace: String = "", name: String,
                change: DiffChange, diff: String = "", truncated: Bool = false, error: String = "") {
        self.group = group
        self.version = version
        self.kind = kind
        self.namespace = namespace
        self.name = name
        self.change = change
        self.diff = diff
        self.truncated = truncated
        self.error = error
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        group = try c.field(.group, "")
        version = try c.field(.version, "")
        kind = try c.field(.kind, "")
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        change = try c.wire(.change)
        diff = try c.field(.diff, "")
        truncated = try c.field(.truncated, false)
        error = try c.field(.error, "")
    }

    private enum CodingKeys: String, CodingKey { case group, version, kind, namespace, name, change, diff, truncated, error }

    /// The diff's lines without its "---"/"+++" header.
    public var lines: [DiffLine] { parseDiffLines(diff) }
}

/// How an object would change, in the order the screen lists them.
public enum DiffChange: String, CaseIterable, Sendable, WireEnum {
    case error, created, changed, deleted, encrypted, ignored, unchanged

    public static var wireFallback: DiffChange { .unchanged }

    /// A reconcile would write or delete something.
    public var isChange: Bool { self == .created || self == .changed || self == .deleted }
}

/// One line of a unified diff.
public struct DiffLine: Equatable, Sendable {
    public enum Kind: Sendable { case hunk, context, added, removed }

    public let kind: Kind
    public let text: String

    public init(_ kind: Kind, _ text: String) {
        self.kind = kind
        self.text = text
    }
}

/// Reads a unified diff ("--- a", "+++ b" header, then "@@ … @@" hunks of " ", "-", "+" lines).
public func parseDiffLines(_ diff: String) -> [DiffLine] {
    guard !diff.isEmpty else { return [] }
    var body = diff
    if body.hasSuffix("\n") { body.removeLast() }
    let lines = body.split(separator: "\n", omittingEmptySubsequences: false).map(String.init)
    // Only the header: a removed line can start with "--- " too.
    let header = lines.count >= 2 && lines[0].hasPrefix("--- ") && lines[1].hasPrefix("+++ ") ? 2 : 0
    return lines.dropFirst(header).map { line in
        if line.hasPrefix("@@") { return DiffLine(.hunk, line) }
        if line.hasPrefix("+") { return DiffLine(.added, String(line.dropFirst())) }
        if line.hasPrefix("-") { return DiffLine(.removed, String(line.dropFirst())) }
        return DiffLine(.context, line.hasPrefix(" ") ? String(line.dropFirst()) : line)
    }
}
