import Foundation

// A file of an app's source edited in the source browser and proposed as a pull (GitHub,
// Gitea) or merge (GitLab) request, as the Go core takes and answers it
// (go/ichorgo/gitforge_propose.go, textdiff.go). Ichor never commits to the tracked branch.
// Same records as Android (model/GitOpsSource.kt).

/// The Argo CD Application or Flux object whose source is browsed: what the pull request's
/// defaults name.
public struct GitOpsAppRef: Hashable, Sendable {
    /// "argocd" or "flux", as GitOpsSource and GitOpsProposalDefaults take it.
    public let tool: String
    public let kind: String
    public let namespace: String
    public let name: String

    public init(tool: String, kind: String, namespace: String, name: String) {
        self.tool = tool
        self.kind = kind
        self.namespace = namespace
        self.name = name
    }

    public static func argo(namespace: String, name: String) -> GitOpsAppRef {
        GitOpsAppRef(tool: "argocd", kind: "Application", namespace: namespace, name: name)
    }

    public static func flux(kind: String, namespace: String, name: String) -> GitOpsAppRef {
        GitOpsAppRef(tool: "flux", kind: kind, namespace: namespace, name: name)
    }

    /// The controller, as a product name (not translated).
    public var toolLabel: String { tool == "flux" ? "Flux" : "Argo CD" }
}

extension GitSource {
    /// The Git host a change is proposed through (with its token), nil when the repository is
    /// not on a forge the app can write to. Also for a Flux source read from its artifact.
    public var proposalHost: String? {
        browsable && !host.isEmpty ? host : nil
    }
}

/// Why a file of a source cannot be edited into a pull request.
public enum SourceEditBlock: Equatable, Sendable {
    /// The repository is not on a forge the app writes to: the browser it is.
    case noForge
    /// No token is saved for the forge's host.
    case noToken(host: String)
}

/// What stops editing a file of source, nil when it can be proposed. hasToken: a token is
/// saved for source's proposalHost; the demo needs none.
public func sourceEditBlock(_ source: GitSource, demo: Bool, hasToken: Bool) -> SourceEditBlock? {
    guard let host = source.proposalHost else { return .noForge }
    return demo || hasToken ? nil : .noToken(host: host)
}

/// The branch a pull request on source goes to: what the app follows when it is a branch
/// ("refs/heads/main" as "main"); nil for a commit, a tag ("v1.2.3", "refs/tags/…"), a semver
/// range or HEAD, which the user then names.
public func proposalBaseRef(for source: GitSource) -> String? {
    var ref = source.tracked.trimmingCharacters(in: .whitespaces)
    if ref.hasPrefix("refs/heads/") {
        ref.removeFirst("refs/heads/".count)
    } else if ref.hasPrefix("refs/") {
        return nil
    }
    guard !ref.isEmpty, ref != "HEAD", !ref.hasPrefix("-"), !ref.contains(".."),
          ref.unicodeScalars.allSatisfy(isBranchScalar) else { return nil }
    if isCommitLike(ref) || isVersionTag(ref) { return nil }
    return ref
}

/// The letters a branch name keeps (the Go core's `^[\w./-]+$`).
private func isBranchScalar(_ s: Unicode.Scalar) -> Bool {
    s.properties.isAlphabetic || ("0"..."9").contains(s) || s == "_" || s == "." || s == "/" || s == "-"
}

/// A full commit sha (SHA-1 or SHA-256).
private func isCommitLike(_ ref: String) -> Bool {
    (ref.count == 40 || ref.count == 64) && ref.allSatisfy(\.isHexDigit)
}

/// "v1.2.3", "1.2", "v2", "1.x": a release tag or a version range rather than a branch.
private func isVersionTag(_ ref: String) -> Bool {
    let version = ref.hasPrefix("v") || ref.hasPrefix("V") ? ref.dropFirst() : Substring(ref)
    guard let first = version.first, first.isNumber else { return false }
    return version.split(separator: "-", maxSplits: 1)[0].allSatisfy { $0.isNumber || $0 == "." || $0 == "x" || $0 == "X" }
}

/// The prefilled pull request (GitOpsProposalDefaults).
public struct GitProposalDefaults: Decodable, Equatable, Sendable {
    public let title: String
    public let message: String
    public let branch: String
    public let body: String

    public init(title: String, message: String, branch: String, body: String) {
        self.title = title
        self.message = message
        self.branch = branch
        self.body = body
    }

    enum CodingKeys: String, CodingKey { case title, message, branch, body }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        title = try c.field(.title, "")
        message = try c.field(.message, "")
        branch = try c.field(.branch, "")
        body = try c.field(.body, "")
    }
}

/// One file committed on a new branch from baseRef, and the request opened from it
/// (GitForgePropose's options).
public struct GitProposal: Codable, Equatable, Sendable {
    public let repoURL: String
    public let baseRef: String
    public let path: String
    public let content: String
    /// The sha the forge gave when the file was read: the optimistic lock ("" for a new file).
    public let fileSha: String
    public let message: String
    /// "" lets the Go core name it.
    public let branch: String
    public let title: String
    public let body: String
    public let draft: Bool

    public init(repoURL: String, baseRef: String, path: String, content: String, fileSha: String, message: String,
                branch: String, title: String, body: String, draft: Bool) {
        self.repoURL = repoURL
        self.baseRef = baseRef
        self.path = path
        self.content = content
        self.fileSha = fileSha
        self.message = message
        self.branch = branch
        self.title = title
        self.body = body
        self.draft = draft
    }

    /// The JSON GitForgePropose takes.
    public func optionsJSON() throws -> String { try TalosJSON.encode(self) }
}

/// The request opened (GitForgePropose's answer).
public struct GitProposalResult: Codable, Equatable, Sendable {
    public let url: String
    public let number: Int
    public let branch: String
    public let commit: String
    /// "github", "gitlab" or "gitea".
    public let forge: String

    public init(url: String, number: Int, branch: String, commit: String = "", forge: String = "") {
        self.url = url
        self.number = number
        self.branch = branch
        self.commit = commit
        self.forge = forge
    }

    enum CodingKeys: String, CodingKey { case url, number, branch, commit, forge }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        url = try c.field(.url, "")
        number = try c.field(.number, 0)
        branch = try c.field(.branch, "")
        commit = try c.field(.commit, "")
        forge = try c.field(.forge, "")
    }
}

/// The forge refused the commit because the file changed on the base since it was read
/// (the Go core's "the file changed on <base> since you opened it: reload and try again"):
/// the app then offers Reload.
public func isStaleProposal(_ message: String) -> Bool {
    message.contains("since you opened it")
}

/// Texts compared as the Go diff does: CRLF and lone CR as "\n".
public func sameSourceText(_ a: String, _ b: String) -> Bool {
    func normalized(_ s: String) -> String {
        s.replacingOccurrences(of: "\r\n", with: "\n").replacingOccurrences(of: "\r", with: "\n")
    }
    return normalized(a) == normalized(b)
}

/// The Kubernetes type a source file declares, for the editor's schema help.
public struct SourceManifestType: Equatable, Sendable {
    /// "" for the core group.
    public let group: String
    public let version: String
    public let kind: String

    public init(group: String, version: String, kind: String) {
        self.group = group
        self.version = version
        self.kind = kind
    }
}

/// The apiVersion and kind at the top level of text, when every document of it declares the
/// same ones; nil for a values file, a kustomization.yaml (kustomize's own types, not in the
/// cluster's schema) or documents of different kinds.
public func sourceManifestType(_ text: String) -> SourceManifestType? {
    var found: [Int: (apiVersion: String, kind: String)] = [:]
    var document = 0
    for raw in text.replacingOccurrences(of: "\r\n", with: "\n").split(separator: "\n", omittingEmptySubsequences: false) {
        let line = String(raw)
        if line.hasPrefix("---") {
            document += 1
            continue
        }
        let entry = found[document] ?? (apiVersion: "", kind: "")
        if let value = topLevelValue(line, key: "apiVersion") {
            found[document] = (apiVersion: value, kind: entry.kind)
        } else if let value = topLevelValue(line, key: "kind") {
            found[document] = (apiVersion: entry.apiVersion, kind: value)
        }
    }
    let types = Set(found.values.map { "\($0.apiVersion) \($0.kind)" })
    guard types.count == 1, let only = found.values.first, !only.apiVersion.isEmpty, !only.kind.isEmpty else { return nil }
    let parts = only.apiVersion.split(separator: "/", maxSplits: 1).map(String.init)
    let group = parts.count == 2 ? parts[0] : ""
    let version = parts.count == 2 ? parts[1] : parts[0]
    if group.hasSuffix("kustomize.config.k8s.io") { return nil }
    return SourceManifestType(group: group, version: version, kind: only.kind)
}

/// The value of `key:` at column 0, without quotes or a trailing comment.
private func topLevelValue(_ line: String, key: String) -> String? {
    guard line.hasPrefix(key + ":") else { return nil }
    var value = line.dropFirst(key.count + 1)
    if let hash = value.range(of: " #") { value = value[..<hash.lowerBound] }
    let trimmed = value.trimmingCharacters(in: .whitespaces).trimmingCharacters(in: CharacterSet(charactersIn: "\"'"))
    return trimmed.isEmpty ? nil : trimmed
}
