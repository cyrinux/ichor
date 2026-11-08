import Foundation
import Observation
import IchorCore

/// The GitOps state kept per cluster on this device, by context fingerprint: the Git repository
/// saved for each cluster and the pull requests opened from the phone. The callers say which
/// cluster they mean (`model.activeSummary`), so this store needs no model.
@Observable
@MainActor
final class ClusterGitStore {
    /// The Git repository saved for each cluster (its Talos and Kubernetes config); no token in
    /// it. Only on this device, and in backups.
    private(set) var repos: [String: ClusterRepo]

    /// The pull requests opened from the phone, per cluster, newest first; no token in them.
    /// Only on this device, and in backups.
    private(set) var proposals: [String: [GitProposalRecord]]

    init() {
        repos = ClusterRepoStore.decode(UserDefaults.standard.data(forKey: ClusterRepoStore.key))
        proposals = ProposalsStore.decode(UserDefaults.standard.data(forKey: ProposalsStore.key))
        // The background monitor saved the proposals' states (merged, closed).
        NotificationCenter.default.addObserver(forName: .proposalsChanged, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.reload() }
        }
    }

    /// The repository of `context`: the one saved, the demo's built-in one, nil without.
    func repo(for context: ContextSummary?) -> ClusterRepo? {
        guard let context else { return nil }
        return context.demo ? .demo : repos[context.fingerprint]
    }

    /// Saves `repo` for `fingerprint`'s cluster; nil, or one without an address, forgets it.
    func setRepo(_ repo: ClusterRepo?, for fingerprint: String) {
        guard !fingerprint.isEmpty else { return }
        var updated = repos
        updated[fingerprint] = repo.flatMap(normalizeClusterRepo)
        store(repos: updated)
    }

    /// `context`'s proposals for tool's kind namespace/name ("" for the repository); the demo's samples.
    func proposals(
        for context: ContextSummary?, tool: String, kind: String, namespace: String, name: String
    ) -> [GitProposalRecord] {
        guard let context else { return [] }
        let all = context.demo ? ProposalsStore.demo : proposals[context.fingerprint] ?? []
        return all.filter { $0.isFor(tool: tool, kind: kind, namespace: namespace, name: name) }
    }

    /// Keeps a pull request opened from the phone for `context` (not in the demo).
    func remember(_ record: GitProposalRecord, for context: ContextSummary?) {
        guard let context, !context.demo, !context.fingerprint.isEmpty else { return }
        var all = storedProposals
        all[context.fingerprint] = ProposalsStore.adding(record, to: all[context.fingerprint] ?? [])
        store(proposals: all)
    }

    /// Forgets a proposal of `context`.
    func forget(_ record: GitProposalRecord, for context: ContextSummary?) {
        guard let context else { return }
        var all = storedProposals
        all[context.fingerprint] = all[context.fingerprint]?.filter { $0.id != record.id }
        if all[context.fingerprint]?.isEmpty == true { all[context.fingerprint] = nil }
        store(proposals: all)
    }

    /// The proposals as stored now: the background monitor writes their states there, so a
    /// change made here starts from them and never puts back an older state.
    private var storedProposals: [String: [GitProposalRecord]] {
        ProposalsStore.decode(UserDefaults.standard.data(forKey: ProposalsStore.key))
    }

    /// Reads the proposals again after the background monitor saved their states.
    func reload() {
        let stored = storedProposals
        if stored != proposals { proposals = stored }
    }

    func store(proposals all: [String: [GitProposalRecord]]) {
        guard all != proposals else { return }
        proposals = all
        UserDefaults.standard.set(ProposalsStore.encode(all), forKey: ProposalsStore.key)
    }

    func store(repos all: [String: ClusterRepo]) {
        guard all != repos else { return }
        repos = all
        UserDefaults.standard.set(ClusterRepoStore.encode(all), forKey: ClusterRepoStore.key)
    }

    /// The clusters changed: what belonged to the others goes.
    func keep(fingerprints: [String]) {
        store(repos: ClusterRepoStore.keep(repos, fingerprints: fingerprints))
        store(proposals: ProposalsStore.keep(storedProposals, fingerprints: fingerprints))
    }
}
