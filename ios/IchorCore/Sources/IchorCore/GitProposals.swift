import Foundation

// The pull requests opened from the phone, kept to tell later whether they were merged and
// whether the cluster runs them (go/ichorgo/gitforge_request.go, kube_gitops_revision.go). Same
// records as Android (model/GitProposalRecord.kt).

/// A pull or merge request opened from the phone: what it changed (`tool`'s `kind`
/// `namespace`/`name`, or the cluster repository when `tool` is ""), where, and what the forge
/// answered. No token: the Git host's is used to read it again.
public struct GitProposalRecord: Codable, Hashable, Sendable, Identifiable {
    public var tool: String
    public var kind: String
    public var namespace: String
    public var name: String
    public var repoURL: String
    public var path: String
    public var title: String
    public var url: String
    public var number: Int
    public var branch: String
    public var commit: String
    public var forge: String
    /// Milliseconds since the epoch, as Android keeps it.
    public var createdAt: Int64

    public init(tool: String, kind: String = "", namespace: String = "", name: String = "", repoURL: String, path: String,
                title: String, url: String, number: Int, branch: String = "", commit: String = "", forge: String = "",
                createdAt: Int64 = Int64(Date().timeIntervalSince1970 * 1000)) {
        self.tool = tool
        self.kind = kind
        self.namespace = namespace
        self.name = name
        self.repoURL = repoURL
        self.path = path
        self.title = title
        self.url = url
        self.number = number
        self.branch = branch
        self.commit = commit
        self.forge = forge
        self.createdAt = createdAt
    }

    /// One record per request.
    public var id: String { "\(repoURL)#\(number)" }
    /// The Git host whose token reads it ("github.com").
    public var host: String { normalizedGitHost(repoURL) }

    /// Made for that target: the app's proposals, or the repository's (tool "").
    public func isFor(tool: String, kind: String, namespace: String, name: String) -> Bool {
        self.tool == tool && self.kind == kind && self.namespace == namespace && self.name == name
    }

    enum CodingKeys: String, CodingKey { case tool, kind, namespace, name, repoURL, path, title, url, number, branch, commit, forge, createdAt }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        tool = try c.field(.tool, "")
        kind = try c.field(.kind, "")
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        repoURL = try c.field(.repoURL, "")
        path = try c.field(.path, "")
        title = try c.field(.title, "")
        url = try c.field(.url, "")
        number = try c.field(.number, 0)
        branch = try c.field(.branch, "")
        commit = try c.field(.commit, "")
        forge = try c.field(.forge, "")
        createdAt = try c.field(.createdAt, Int64(0))
    }
}

/// GitForgeRequest: the request's state on the forge, its head and merge commits.
public struct GitRequestState: Decodable, Hashable, Sendable {
    public let state: String
    public let title: String
    public let url: String
    public let headSha: String
    public let mergeSha: String
    public let draft: Bool

    public var merged: Bool { state == "merged" }
    public var open: Bool { state == "open" }

    public init(state: String, title: String = "", url: String = "", headSha: String = "", mergeSha: String = "", draft: Bool = false) {
        self.state = state
        self.title = title
        self.url = url
        self.headSha = headSha
        self.mergeSha = mergeSha
        self.draft = draft
    }

    enum CodingKeys: String, CodingKey { case state, title, url, headSha, mergeSha, draft }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        state = try c.field(.state, "")
        title = try c.field(.title, "")
        url = try c.field(.url, "")
        headSha = try c.field(.headSha, "")
        mergeSha = try c.field(.mergeSha, "")
        draft = try c.field(.draft, false)
    }
}

/// GitOpsRevisionRuns: whether the app runs a commit, the one it runs, whether it is synced.
public struct RevisionRuns: Decodable, Hashable, Sendable {
    public let runs: Bool
    public let revision: String
    public let synced: Bool

    public init(runs: Bool = false, revision: String = "", synced: Bool = false) {
        self.runs = runs
        self.revision = revision
        self.synced = synced
    }

    enum CodingKeys: String, CodingKey { case runs, revision, synced }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        runs = try c.field(.runs, false)
        revision = try c.field(.revision, "")
        synced = try c.field(.synced, false)
    }
}

/// How a proposal reads on its row.
public enum ProposalStatus: Sendable {
    case open, draft, closed, merged, deployed, otherRevision, unknown
}

/// The row's status from the forge's state and, for a merged request, what the app runs (nil:
/// not asked, for the repository or a failure): deployed when it runs the merge commit or the
/// head; another revision when it runs a commit that is not this one (a newer one, or this one
/// is not applied yet: no commit graph tells which).
public func proposalStatus(_ state: GitRequestState?, _ runs: RevisionRuns?) -> ProposalStatus {
    guard let state else { return .unknown }
    if state.open { return state.draft ? .draft : .open }
    guard state.merged else { return .closed }
    guard let runs else { return .merged }
    if runs.runs { return .deployed }
    return runs.revision.isEmpty ? .merged : .otherRevision
}

/// The proposals kept per cluster, by context fingerprint, newest first and 50 at most.
public enum ProposalsStore {
    public static let key = "proposals"
    public static let maxPerCluster = 50

    public static func decode(_ data: Data?) -> [String: [GitProposalRecord]] {
        guard let data, let saved = try? JSONDecoder().decode([String: [GitProposalRecord]].self, from: data) else { return [:] }
        return saved
    }

    public static func encode(_ proposals: [String: [GitProposalRecord]]) -> Data? {
        proposals.isEmpty ? nil : try? JSONEncoder().encode(proposals)
    }

    /// The records with record first, a request kept once, 50 at most.
    public static func adding(_ record: GitProposalRecord, to records: [GitProposalRecord]) -> [GitProposalRecord] {
        Array(([record] + records.filter { $0.id != record.id }).prefix(maxPerCluster))
    }

    /// The proposals of the clusters still among fingerprints: a removed cluster's go with it.
    public static func keep(_ saved: [String: [GitProposalRecord]], fingerprints: [String]) -> [String: [GitProposalRecord]] {
        let known = Set(fingerprints.filter { !$0.isEmpty })
        return saved.filter { known.contains($0.key) }
    }

    /// The demo cluster's sample proposals on its Argo CD app argocd/hello-ichor: #1 open, #2 merged and deployed.
    public static let demo: [GitProposalRecord] = [
        GitProposalRecord(tool: "argocd", kind: "Application", namespace: "argocd", name: "hello-ichor", repoURL: ClusterRepo.demo.repoURL,
                          path: "apps/hello-ichor/deployment.yaml", title: "Bump podinfo to 6.7.1",
                          url: "https://git.home.example/example-org/cluster-config/pulls/1", number: 1, forge: "demo", createdAt: 1_760_860_800_000),
        GitProposalRecord(tool: "argocd", kind: "Application", namespace: "argocd", name: "hello-ichor", repoURL: ClusterRepo.demo.repoURL,
                          path: "apps/hello-ichor/deployment.yaml", title: "Scale podinfo to 3",
                          url: "https://git.home.example/example-org/cluster-config/pulls/2", number: 2, forge: "demo", createdAt: 1_760_774_400_000),
    ]
}
