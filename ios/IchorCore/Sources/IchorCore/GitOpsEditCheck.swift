import Foundation

// Mirrors go/ichorgo/gitops_check.go.

/// A build error, with the repository file and line it names (0 when none).
public struct EditCheckError: Decodable, Hashable, Sendable {
    public let message: String
    public let path: String
    public let line: Int

    private enum CodingKeys: String, CodingKey { case message, path, line }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        message = try c.field(.message, "")
        path = try c.field(.path, "")
        line = try c.field(.line, 0)
    }
}

/// An edit checked before it is proposed: `kind` is kustomize (the nearest kustomization built
/// before and after), plain (the file's own documents), helm (not rendered on the phone) or
/// none; when `ok`, `resources` are the rendered objects that change (`unchanged` counted).
public struct GitOpsEditCheck: Decodable, Sendable {
    public let sha: String
    public let kind: String
    public let dir: String
    public let ok: Bool
    public let errors: [EditCheckError]
    public let resources: [KubeDiffResource]
    public let unchanged: Int
    /// The build fails at the commit already, the same way: not the edit's doing.
    public let broken: Bool
    public let note: String

    private enum CodingKeys: String, CodingKey { case sha, kind, dir, ok, errors, diff, unchanged, broken, note }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        sha = try c.field(.sha, "")
        kind = try c.field(.kind, "")
        dir = try c.field(.dir, "")
        ok = try c.field(.ok, false)
        errors = try c.field(.errors, [])
        resources = try c.decodeIfPresent(FluxDiff.self, forKey: .diff)?.resources ?? []
        unchanged = try c.field(.unchanged, 0)
        broken = try c.field(.broken, false)
        note = try c.field(.note, "")
    }

    /// The edit breaks the build: what the review warns about.
    public var fails: Bool { !ok && !broken }

    /// Something was built or parsed: the objects say what changes.
    public var built: Bool { ok && (kind == "kustomize" || kind == "plain") }

    /// The minimal Flux spec of an app's source: its path, built as kustomize-controller would.
    public static func fluxSpec(path: String) -> String {
        let data = (try? JSONEncoder().encode(["path": path])) ?? Data("{}".utf8)
        return String(decoding: data, as: UTF8.self)
    }
}
