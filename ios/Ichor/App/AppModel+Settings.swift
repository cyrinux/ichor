import Foundation
import Observation
import IchorCore

// Per-cluster settings (VPN only, repository, proposals, background watch) and what follows them
// (widget, Spotlight). The values themselves are SettingsStore's.
extension AppModel {
    /// Whether the cluster on screen is set to be reached over a VPN only and none is up.
    var vpnHeldBack: Bool {
        heldBackForVpn(vpnOnly: settings.vpnOnly, fingerprint: activeSummary?.fingerprint, vpnUp: VpnMonitor.shared.up)
    }

    /// Sets whether `context` is reached over a VPN only; the cluster on screen reloads (with
    /// the VPN off, the screens then say to connect it).
    func setVpnOnly(_ on: Bool, for context: ContextSummary) {
        guard !context.fingerprint.isEmpty else { return }
        let fingerprint = context.fingerprint
        settings.storeVpnOnly(on ? settings.vpnOnly.union([fingerprint]) : settings.vpnOnly.subtracting([fingerprint]))
        if context.fingerprint == activeSummary?.fingerprint { dataGeneration += 1 }
    }

    /// The repository of the cluster on screen: the one saved, the demo's built-in one, nil without.
    var activeClusterRepo: ClusterRepo? {
        guard let active = activeSummary else { return nil }
        return active.demo ? .demo : clusterRepos[active.fingerprint]
    }

    /// Saves `repo` for `fingerprint`'s cluster; nil, or one without an address, forgets it.
    func setClusterRepo(_ repo: ClusterRepo?, for fingerprint: String) {
        guard !fingerprint.isEmpty else { return }
        var repos = clusterRepos
        repos[fingerprint] = repo.flatMap(normalizeClusterRepo)
        storeClusterRepos(repos)
    }

    /// The cluster on screen's proposals for tool's kind namespace/name ("" for the repository); the demo's samples.
    func proposals(tool: String, kind: String, namespace: String, name: String) -> [GitProposalRecord] {
        guard let active = activeSummary else { return [] }
        let all = active.demo ? ProposalsStore.demo : proposals[active.fingerprint] ?? []
        return all.filter { $0.isFor(tool: tool, kind: kind, namespace: namespace, name: name) }
    }

    /// Keeps a pull request opened from the phone for the cluster on screen (not in the demo).
    func rememberProposal(_ record: GitProposalRecord) {
        guard let active = activeSummary, !active.demo, !active.fingerprint.isEmpty else { return }
        var all = storedProposals
        all[active.fingerprint] = ProposalsStore.adding(record, to: all[active.fingerprint] ?? [])
        storeProposals(all)
    }

    /// Forgets a proposal of the cluster on screen.
    func forgetProposal(_ record: GitProposalRecord) {
        guard let active = activeSummary else { return }
        var all = storedProposals
        all[active.fingerprint] = all[active.fingerprint]?.filter { $0.id != record.id }
        if all[active.fingerprint]?.isEmpty == true { all[active.fingerprint] = nil }
        storeProposals(all)
    }

    /// The proposals as stored now: the background monitor writes their states there, so a
    /// change made here starts from them and never puts back an older state.
    var storedProposals: [String: [GitProposalRecord]] {
        ProposalsStore.decode(UserDefaults.standard.data(forKey: ProposalsStore.key))
    }

    /// Reads the proposals again after the background monitor saved their states.
    func reloadProposals() {
        let stored = storedProposals
        if stored != proposals { proposals = stored }
    }

    // Internal, not private: AppModel+Clusters.swift uses it.

    func storeProposals(_ all: [String: [GitProposalRecord]]) {
        guard all != proposals else { return }
        proposals = all
        UserDefaults.standard.set(ProposalsStore.encode(all), forKey: ProposalsStore.key)
    }

    // Internal, not private: AppModel+Clusters.swift uses it.

    func storeClusterRepos(_ repos: [String: ClusterRepo]) {
        guard repos != clusterRepos else { return }
        clusterRepos = repos
        UserDefaults.standard.set(ClusterRepoStore.encode(repos), forKey: ClusterRepoStore.key)
    }

    /// Whether the background checks watch `context`'s cluster (off on any of its contexts: not).
    func watchesInBackground(_ context: ContextSummary) -> Bool {
        watchedInBackground(context, contexts: summary?.contexts ?? [], unwatched: settings.monitorUnwatched)
    }

    /// Turns the background checks of `context`'s cluster on or off.
    func setWatchInBackground(_ on: Bool, for context: ContextSummary) {
        settings.storeMonitorUnwatched(
            settingWatched(on, for: context, contexts: summary?.contexts ?? [], unwatched: settings.monitorUnwatched))
    }

    /// Whether `context`'s cluster records actions (a Kubernetes Event per change).
    func recordsActions(_ context: ContextSummary) -> Bool {
        clusterFlagged(context, contexts: summary?.contexts ?? [], members: settings.auditEventClusters)
    }

    /// Turns "Record actions in the cluster" on or off for `context`'s cluster (all its contexts).
    func setRecordActions(_ on: Bool, for context: ContextSummary) {
        settings.storeAuditEventClusters(
            settingClusterFlag(
                on, for: context, contexts: summary?.contexts ?? [],
                members: settings.auditEventClusters))
    }

    /// Tells the widget which clusters it can show, named as on screen, and which one is active
    /// (what a widget with no cluster chosen shows).
    // Internal, not private: AppModel+Clusters.swift uses it.
    func publishWidgetClusters() {
        // A widget set to a cluster opens that cluster's screen (a share link, routed after unlock).
        let clusters =
            summary.map { summary in
                widgetClusters(summary.contexts, active: activeContext, labels: labels) { context in
                    guard !context.clusterID.isEmpty else { return nil }
                    var target = ShareTarget.screen(.cluster)
                    target.cluster = context.clusterID
                    return TalosClient.shareLink(for: target).flatMap { appShareLink(web: $0.absoluteString) }
                }
            } ?? []
        SharedStore.publish(clusters: clusters, active: activeSummary.map(monitorClusterKey))
    }

    /// Spotlight lists its items outside the app: only with the last known state kept, no app
    /// lock and no screenshot mode (any of them on deletes what was indexed).
    // Internal, not private: AppModel+Clusters.swift uses it.
    func updateSpotlight() {
        SpotlightIndex.update(
            summary: summary, active: activeContext, labels: labels,
            enabled: settings.keepLastKnown && !lock.enabled && !settings.privacyMask)
    }

    /// The VPN came up: a VPN-only cluster on screen reloads.
    // Internal, not private: AppModel.swift uses it.
    func reloadIfHeldBack() {
        if let shown = activeSummary?.fingerprint, settings.vpnOnly.contains(shown) { dataGeneration += 1 }
    }
}
