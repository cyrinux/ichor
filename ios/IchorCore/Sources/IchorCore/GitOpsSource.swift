import Foundation

// Where an Argo CD or Flux app's files are, and the files themselves, as the Go core answers
// (go/ichorgo/kube_gitops_source.go, kube_gitops_tree.go, gitforge.go). A Flux source is read
// from the artifact the source-controller holds (no token); any other Git source through the
// Git host's API with the token saved in Settings (GitHosts.swift). Same records as Android
// (model/GitOpsSource.kt).

/// How a source's files are laid out: declared kustomize or helm, a plain directory, a packaged
/// Helm chart (no Git tree), or an Argo CD source only read for its value files.
public enum GitSourceKind: String, CaseIterable, Codable, Hashable, Sendable, WireEnum {
    case kustomize
    case helm
    case directory
    case chart
    case values

    public static let wireFallback = GitSourceKind.directory
}

/// One source of an app: the repository, what it follows, the commit the cluster runs and the
/// directory it deploys.
public struct GitSource: Decodable, Hashable, Sendable {
    public let repoURL: String
    /// The repository's page, "" when it is not a Git repository on a web host.
    public let webURL: String
    /// The branch, tag, semver range or commit the app follows.
    public let tracked: String
    /// The commit the cluster runs, "" when unknown.
    public let revision: String
    public let path: String
    public let kind: GitSourceKind
    public let chart: String
    public let valueFiles: [String]
    /// The files can be read through the Git host's API.
    public let browsable: Bool
    /// The files can be read from fluxSource's artifact, with no token.
    public let fluxArtifact: Bool
    public let fluxSource: FluxRef?

    public init(
        repoURL: String, webURL: String = "", tracked: String = "", revision: String = "", path: String = "",
        kind: GitSourceKind = .directory, chart: String = "", valueFiles: [String] = [],
        browsable: Bool = false, fluxArtifact: Bool = false, fluxSource: FluxRef? = nil
    ) {
        self.repoURL = repoURL
        self.webURL = webURL
        self.tracked = tracked
        self.revision = revision
        self.path = path
        self.kind = kind
        self.chart = chart
        self.valueFiles = valueFiles
        self.browsable = browsable
        self.fluxArtifact = fluxArtifact
        self.fluxSource = fluxSource
    }

    enum CodingKeys: String, CodingKey {
        case repoURL, webURL, tracked, revision, path, kind, chart, valueFiles, browsable, fluxArtifact, fluxSource
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        repoURL = try c.field(.repoURL, "")
        webURL = try c.field(.webURL, "")
        tracked = try c.field(.tracked, "")
        revision = try c.field(.revision, "")
        path = try c.field(.path, "")
        kind = try c.wire(.kind)
        chart = try c.field(.chart, "")
        valueFiles = try c.field(.valueFiles, [String]())
        browsable = try c.field(.browsable, false)
        fluxArtifact = try c.field(.fluxArtifact, false)
        fluxSource = try c.decodeIfPresent(FluxRef.self, forKey: .fluxSource)
    }

    /// The ref the forge reads: the commit the cluster runs, else what the app follows.
    public var ref: String { revision.isEmpty ? tracked : revision }

    /// The Git host of webURL ("github.com"), "" without one.
    public var host: String { normalizedGitHost(webURL) }

    /// How its files are read.
    public var read: GitSourceRead {
        if fluxArtifact, let fluxSource { return .flux(fluxSource) }
        if browsable, !host.isEmpty { return .forge(host: host, ref: ref) }
        return .unreadable
    }

    /// A packaged Helm chart: the row names it, there is no tree to open.
    public var isChart: Bool { kind == .chart }

    /// Shown as a Source row of the app: every source but one only read for its value files.
    public var hasRow: Bool { kind != .values }

    /// The row opens the browser.
    public var canOpen: Bool { !isChart && read != .unreadable }

    /// The directory the browser opens at: the deployed path, without "./" or slashes around it.
    public var startDir: String { cleanSourcePath(path) }
}

/// Where a source's files come from.
public enum GitSourceRead: Hashable, Sendable {
    /// The artifact of this Flux source (GitRepository, OCIRepository or Bucket): no token.
    case flux(FluxRef)
    /// The Git host's API, at ref, with the token saved for host.
    case forge(host: String, ref: String)
    /// Neither: a chart repository, an OCI registry, a local path.
    case unreadable
}

/// What GitOpsSource answers: one entry per source of the app.
public struct GitOpsSource: Decodable, Hashable, Sendable {
    public let sources: [GitSource]
    public let note: String

    public init(sources: [GitSource], note: String = "") {
        self.sources = sources
        self.note = note
    }

    enum CodingKeys: String, CodingKey { case sources, note }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        sources = try c.field(.sources, [GitSource]())
        note = try c.field(.note, "")
    }

    /// The sources shown as rows, with their index in `sources`.
    public var rows: [(index: Int, source: GitSource)] {
        sources.enumerated().filter { $0.element.hasRow }.map { (index: $0.offset, source: $0.element) }
    }
}

/// One entry of a directory listing.
public struct GitEntry: Decodable, Hashable, Identifiable, Sendable {
    /// The full path from the repository root.
    public let path: String
    public let isDir: Bool
    public let size: Int64
    /// A file SOPS encrypts, by its name.
    public let sops: Bool

    public var id: String { path }

    /// The last part of path.
    public var name: String { path.split(separator: "/").last.map(String.init) ?? path }

    public init(path: String, isDir: Bool, size: Int64 = 0, sops: Bool = false) {
        self.path = path
        self.isDir = isDir
        self.size = size
        self.sops = sops
    }

    enum CodingKeys: String, CodingKey { case path, type, size, sops }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        path = try c.field(.path, "")
        isDir = try c.field(.type, "file") == "dir"
        size = try c.field(.size, Int64(0))
        sops = try c.field(.sops, false)
    }
}

/// A directory of a source, at a ref.
public struct GitTree: Decodable, Hashable, Sendable {
    public let ref: String
    /// The commit read, "" when the host does not say.
    public let sha: String
    public let entries: [GitEntry]

    public init(ref: String, sha: String = "", entries: [GitEntry]) {
        self.ref = ref
        self.sha = sha
        self.entries = entries
    }

    enum CodingKeys: String, CodingKey { case ref, sha, entries }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        ref = try c.field(.ref, "")
        sha = try c.field(.sha, "")
        entries = try c.field(.entries, [GitEntry]())
    }

    /// Directories first, then files, each by name (the Go core sorts them already).
    public var sorted: [GitEntry] {
        entries.sorted { $0.isDir != $1.isDir ? $0.isDir : $0.path < $1.path }
    }
}

/// One file of a source; a file over 1 MiB comes back truncated, without content.
public struct GitFile: Decodable, Hashable, Sendable {
    public let path: String
    public let sha: String
    public let size: Int64
    public let content: String
    public let truncated: Bool

    public init(path: String, sha: String = "", size: Int64 = 0, content: String = "", truncated: Bool = false) {
        self.path = path
        self.sha = sha
        self.size = size
        self.content = content
        self.truncated = truncated
    }

    enum CodingKeys: String, CodingKey { case path, sha, size, content, truncated }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        path = try c.field(.path, "")
        sha = try c.field(.sha, "")
        size = try c.field(.size, Int64(0))
        content = try c.field(.content, "")
        truncated = try c.field(.truncated, false)
    }
}

/// A path as the browser keeps it: no "./", no leading, trailing or doubled slashes; "" is the root.
public func cleanSourcePath(_ path: String) -> String {
    path.split(separator: "/").filter { $0 != "." && !$0.isEmpty }.joined(separator: "/")
}

/// One step of the breadcrumb above a directory: its name and its full path.
public struct SourceCrumb: Hashable, Sendable {
    public let name: String
    public let path: String

    public init(name: String, path: String) {
        self.name = name
        self.path = path
    }
}

/// The directories from the root down to dir ("apps/podinfo" → apps, apps/podinfo); the root
/// itself is not in it (the app names it).
public func sourceBreadcrumb(_ dir: String) -> [SourceCrumb] {
    let parts = cleanSourcePath(dir).split(separator: "/").map(String.init)
    return parts.indices.map { SourceCrumb(name: parts[$0], path: parts[...$0].joined(separator: "/")) }
}

/// The Source row's label: "apps/podinfo @ 1a2b3c4" (the root as "/", what the app follows
/// when the commit is unknown).
public func sourceRowLabel(_ source: GitSource) -> String {
    let dir = cleanSourcePath(source.path)
    let at = source.revision.isEmpty ? source.tracked : shortRevision(source.revision)
    let path = dir.isEmpty ? "/" : dir
    return at.isEmpty ? path : "\(path) @ \(at)"
}

/// The page of path at ref on the forge of webURL (a file, or a directory when dir), "" without
/// webURL. GitLab and Gitea name their pages differently from GitHub; auto tells GitLab from
/// the host name and reads anything else as GitHub.
public func gitWebURL(_ webURL: String, kind: GitHostKind, ref: String, path: String, dir: Bool) -> String {
    var base = webURL.trimmingCharacters(in: .whitespaces)
    while base.hasSuffix("/") { base.removeLast() }
    if base.hasSuffix(".git") { base.removeLast(4) }
    guard !base.isEmpty else { return "" }
    let clean = cleanSourcePath(path)
    guard !ref.isEmpty else { return base }
    let forge: GitHostKind = kind == .auto ? (normalizedGitHost(base).contains("gitlab") ? .gitlab : .github) : kind
    let page: String
    switch forge {
    case .gitlab:
        page = "/-/\(dir ? "tree" : "blob")/\(ref)"
    case .gitea:
        let commit = ref.count >= 12 && ref.allSatisfy(\.isHexDigit)
        page = "/src/\(commit ? "commit" : "branch")/\(ref)"
    case .github, .auto:
        page = "/\(dir ? "tree" : "blob")/\(ref)"
    }
    return clean.isEmpty ? base + page : "\(base)\(page)/\(clean)"
}

// MARK: - The file that declares a resource

/// One document of a source that declares a resource: the file (from the repository root),
/// the line of its metadata.name, and which document of the file it is.
public struct ManifestMatch: Codable, Hashable, Identifiable, Sendable {
    public let path: String
    /// 1-based, as the file viewer numbers its lines.
    public let line: Int
    public let documentIndex: Int

    public var id: String { "\(path)#\(documentIndex)" }

    public init(path: String, line: Int, documentIndex: Int = 0) {
        self.path = path
        self.line = line
        self.documentIndex = documentIndex
    }

    enum CodingKeys: String, CodingKey { case path, line, documentIndex }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        path = try c.field(.path, "")
        line = try c.field(.line, 0)
        documentIndex = try c.field(.documentIndex, 0)
    }
}

/// What GitOpsSourceFind answers: the documents declaring the resource, how many files were
/// read, and why there is no match when it can say (a Helm-rendered name, a cut search).
public struct GitOpsSourceFind: Decodable, Hashable, Sendable {
    public let matches: [ManifestMatch]
    public let searched: Int
    public let note: String

    public init(matches: [ManifestMatch], searched: Int = 0, note: String = "") {
        self.matches = matches
        self.searched = searched
        self.note = note
    }

    enum CodingKeys: String, CodingKey { case matches, searched, note }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        matches = try c.field(.matches, [ManifestMatch]())
        searched = try c.field(.searched, 0)
        note = try c.field(.note, "")
    }

    /// What the diff does with the answer: open the one file, offer the files, or say why not.
    public var outcome: SourceFindOutcome {
        switch matches.count {
        case 0: .none(note: note)
        case 1: .open(matches[0])
        default: .choose(matches)
        }
    }
}

/// The diff's next step after a search.
public enum SourceFindOutcome: Hashable, Sendable {
    case open(ManifestMatch)
    case choose([ManifestMatch])
    /// No file declares it; note is the Go core's reason, "" when it gave none.
    case none(note: String)
}

extension GitOpsSource {
    /// The source a diff searches for a resource's file: the first with a Git tree the app can
    /// read (not a packaged chart, not a source only read for its value files), with its index
    /// in `sources` (what GitOpsSourceFind takes).
    public var firstBrowsable: (index: Int, source: GitSource)? {
        // A Helm source only gives the "rendered by Helm" note: another readable source comes first.
        rows.first { $0.source.canOpen && $0.source.kind != .helm } ?? rows.first { $0.source.canOpen }
    }
}
