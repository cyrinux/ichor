import Foundation

// A cluster's own Git repository (its Talos patches, its Kubernetes tree), saved per cluster
// and opened from the root in the source browser, as the Go core reads it
// (go/ichorgo/gitforge_repo.go). Same records as Android (model/ClusterRepo.kt).

/// Where the cluster's repository is: its address, the branch read ("" for the default one)
/// and the two directories of a monorepo. No secret: the token stays with its Git host.
public struct ClusterRepo: Codable, Hashable, Sendable {
    public var repoURL: String
    public var branch: String
    public var talosDir: String
    public var kubernetesDir: String

    public init(repoURL: String, branch: String = "", talosDir: String = "", kubernetesDir: String = "") {
        self.repoURL = repoURL
        self.branch = branch
        self.talosDir = talosDir
        self.kubernetesDir = kubernetesDir
    }

    enum CodingKeys: String, CodingKey {
        case repoURL, branch, talosDir, kubernetesDir
    }

    // Lenient: Android leaves blank fields out of a backup.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        repoURL = try c.field(.repoURL, "")
        branch = try c.field(.branch, "")
        talosDir = try c.field(.talosDir, "")
        kubernetesDir = try c.field(.kubernetesDir, "")
    }

    /// The Git host whose token reads it ("github.com").
    public var host: String { normalizedGitHost(repoURL) }

    /// The demo cluster's built-in repository (demoClusterRepoURL in the Go core).
    public static let demo = ClusterRepo(
        repoURL: "https://git.home.example/example-org/cluster-config",
        talosDir: "talos", kubernetesDir: "kubernetes")
}

/// What GitForgeRepo tells of a repository: its default branch, the head of the branch read
/// and whether the token can push.
public struct GitRepoInfo: Decodable, Equatable, Sendable {
    public let defaultBranch: String
    public let branch: String
    public let headSha: String
    public let webURL: String
    public let isPrivate: Bool
    public let canPush: Bool
    public let forge: String

    public init(
        defaultBranch: String = "", branch: String = "", headSha: String = "", webURL: String = "",
        isPrivate: Bool = false, canPush: Bool = false, forge: String = ""
    ) {
        self.defaultBranch = defaultBranch
        self.branch = branch
        self.headSha = headSha
        self.webURL = webURL
        self.isPrivate = isPrivate
        self.canPush = canPush
        self.forge = forge
    }

    enum CodingKeys: String, CodingKey {
        case defaultBranch, branch, headSha, webURL, canPush, forge
        case isPrivate = "private"
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        defaultBranch = try c.field(.defaultBranch, "")
        branch = try c.field(.branch, "")
        headSha = try c.field(.headSha, "")
        webURL = try c.field(.webURL, "")
        isPrivate = try c.field(.isPrivate, false)
        canPush = try c.field(.canPush, false)
        forge = try c.field(.forge, "")
    }

    /// The crumb of the repository root: "main @ 1a2b3c4".
    public var crumb: String {
        let sha = shortRevision(headSha)
        return sha.isEmpty ? branch : "\(branch) @ \(sha)"
    }
}

/// A directory as typed: trimmed of spaces and of its leading and trailing slashes.
private func cleanRepoDir(_ dir: String) -> String {
    dir.trimmingCharacters(in: .whitespaces).trimmingCharacters(in: CharacterSet(charactersIn: "/"))
}

/// repo as typed, trimmed and its directories cleaned; nil without an address (nothing to save).
public func normalizeClusterRepo(_ repo: ClusterRepo) -> ClusterRepo? {
    let url = repo.repoURL.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !url.isEmpty else { return nil }
    return ClusterRepo(
        repoURL: url, branch: repo.branch.trimmingCharacters(in: .whitespaces),
        talosDir: cleanRepoDir(repo.talosDir), kubernetesDir: cleanRepoDir(repo.kubernetesDir))
}

/// The https page of a repository address typed as https, ssh or scp-like (git@host:org/repo.git).
public func repoWebURL(_ repoURL: String) -> String {
    let host = normalizedGitHost(repoURL)
    guard !host.isEmpty else { return "" }
    let raw = repoURL.trimmingCharacters(in: .whitespaces)
    var path = ""
    if let scheme = raw.range(of: "://") {
        let rest = raw[scheme.upperBound...]
        path = rest.firstIndex(of: "/").map { String(rest[rest.index(after: $0)...]) } ?? ""
    } else if let colon = raw.firstIndex(of: ":") {
        path = String(raw[raw.index(after: colon)...])
    }
    path = path.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
    if path.hasSuffix(".git") { path.removeLast(4) }
    return path.isEmpty ? "" : "https://\(host)/\(path)"
}

/// repo as a source of the source browser: the whole repository from its root, read through
/// the forge at info's head commit (the viewer's "at commit" and the file sha checks work).
public func repoSource(_ repo: ClusterRepo, info: GitRepoInfo) -> GitSource {
    let tracked = [info.branch, repo.branch, info.defaultBranch].first { !$0.isEmpty } ?? ""
    return GitSource(
        repoURL: repo.repoURL, webURL: info.webURL.isEmpty ? repoWebURL(repo.repoURL) : info.webURL,
        tracked: tracked, revision: info.headSha, path: "", kind: .directory, browsable: true)
}

/// The repositories saved per cluster fingerprint, as UserDefaults keeps them (JSON).
public enum ClusterRepoStore {
    public static let key = "clusterRepos"

    public static func decode(_ data: Data?) -> [String: ClusterRepo] {
        guard let data, let saved = try? JSONDecoder().decode([String: ClusterRepo].self, from: data) else {
            return [:]
        }
        return saved.compactMapValues(normalizeClusterRepo)
    }

    public static func encode(_ repos: [String: ClusterRepo]) -> Data? {
        repos.isEmpty ? nil : try? JSONEncoder().encode(repos)
    }

    /// The repositories of the clusters still among fingerprints: a removed cluster's goes with it.
    public static func keep(_ saved: [String: ClusterRepo], fingerprints: [String]) -> [String: ClusterRepo] {
        let known = Set(fingerprints.filter { !$0.isEmpty })
        return saved.filter { known.contains($0.key) }
    }
}
