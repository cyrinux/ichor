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
    /// The last state the background monitor read on the forge ("open", "merged", "closed"; ""
    /// before any, or for records kept before it was read) and when (milliseconds, 0 for none).
    public var state: String
    public var stateAt: Int64

    public init(tool: String, kind: String = "", namespace: String = "", name: String = "", repoURL: String, path: String,
                title: String, url: String, number: Int, branch: String = "", commit: String = "", forge: String = "",
                createdAt: Int64 = Int64(Date().timeIntervalSince1970 * 1000), state: String = "", stateAt: Int64 = 0) {
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
        self.state = state
        self.stateAt = stateAt
    }

    /// One record per request.
    public var id: String { "\(repoURL)#\(number)" }
    /// The Git host whose token reads it ("github.com").
    public var host: String { normalizedGitHost(repoURL) }

    /// Made for that target: the app's proposals, or the repository's (tool "").
    public func isFor(tool: String, kind: String, namespace: String, name: String) -> Bool {
        self.tool == tool && self.kind == kind && self.namespace == namespace && self.name == name
    }

    enum CodingKeys: String, CodingKey { case tool, kind, namespace, name, repoURL, path, title, url, number, branch, commit, forge, createdAt, state, stateAt }

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
        state = try c.field(.state, "")
        stateAt = try c.field(.stateAt, Int64(0))
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

    /// The records with record first, a request kept once, 50 at most. A new record is open
    /// from now on, so the monitor tells when it is merged or closed.
    public static func adding(_ record: GitProposalRecord, to records: [GitProposalRecord],
                              now: Int64 = Int64(Date().timeIntervalSince1970 * 1000)) -> [GitProposalRecord] {
        var record = record
        if record.state.isEmpty {
            record.state = "open"
            record.stateAt = now
        }
        return Array(([record] + records.filter { $0.id != record.id }).prefix(maxPerCluster))
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

// MARK: - Merged and closed alerts

/// One item of GitForgeRequestStates; a non-empty error means it could not be read (empty state).
public struct RequestStateItem: Codable, Hashable, Sendable {
    public let repoURL: String
    public let number: Int
    public let state: String
    public let mergedAt: String
    public let closedAt: String
    public let sha: String
    public let error: String

    public init(repoURL: String, number: Int, state: String, mergedAt: String = "", closedAt: String = "", sha: String = "", error: String = "") {
        self.repoURL = repoURL
        self.number = number
        self.state = state
        self.mergedAt = mergedAt
        self.closedAt = closedAt
        self.sha = sha
        self.error = error
    }

    enum CodingKeys: String, CodingKey { case repoURL, number, state, mergedAt, closedAt, sha, error }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        repoURL = try c.field(.repoURL, "")
        number = try c.field(.number, 0)
        state = try c.field(.state, "")
        mergedAt = try c.field(.mergedAt, "")
        closedAt = try c.field(.closedAt, "")
        sha = try c.field(.sha, "")
        error = try c.field(.error, "")
    }
}

/// An open proposal the forge now says is merged or closed: one notification.
public struct ProposalChange: Hashable, Sendable {
    public let record: GitProposalRecord
    public let state: String

    /// Stable across runs, so the notification replaces itself rather than repeating.
    public var alertKey: String { "proposal:\(record.id)" }

    /// The alert in English (the app words it in the user's language): a GitOps one, see alertKind.
    public var alert: Alert {
        let what = state == "merged" ? "merged" : "closed"
        return Alert(key: alertKey, title: "Your pull request #\(record.number) was \(what)", text: record.title, problem: true)
    }
}

/// The most requests one GitForgeRequestStates call reads.
public let proposalStatesBatch = 20

/// The records the monitor asks the forge about, by host, newest first and 20 per host at
/// most: those still open or never read, outside the demo.
public func proposalsToCheck(_ records: [GitProposalRecord]) -> [String: [GitProposalRecord]] {
    let open = records.filter { ($0.state.isEmpty || $0.state == "open") && $0.forge != "demo" && $0.number > 0 }
        .sorted { $0.createdAt > $1.createdAt }
    return Dictionary(grouping: open, by: \.host).mapValues { Array($0.prefix(proposalStatesBatch)) }
}

/// The records with the forge's answers applied: a record that was open and is now merged or
/// closed is a change (told once: its state is kept); one never read takes its state silently
/// (the first read is a baseline); an item that could not be read changes nothing.
public func proposalStateChanges(records: [GitProposalRecord], states: [RequestStateItem], now: Int64)
    -> (records: [GitProposalRecord], changes: [ProposalChange]) {
    var answers: [String: String] = [:]
    for item in states where item.error.isEmpty && !item.state.isEmpty {
        answers["\(item.repoURL)#\(item.number)"] = item.state
    }
    var changes: [ProposalChange] = []
    let updated = records.map { record -> GitProposalRecord in
        guard let state = answers[record.id], state != record.state else { return record }
        var next = record
        next.state = state
        next.stateAt = now
        if record.state == "open" && (state == "merged" || state == "closed") {
            changes.append(ProposalChange(record: next, state: state))
        }
        return next
    }
    return (updated, changes)
}
