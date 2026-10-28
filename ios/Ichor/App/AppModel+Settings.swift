import Foundation
import Observation
import IchorCore

// Per-cluster settings (VPN only, repository, proposals, background watch), what follows them
// (widget, Spotlight), keeping the last known state, and the screenshot mode.
extension AppModel {
    /// Whether the cluster on screen is set to be reached over a VPN only and none is up.
    var vpnHeldBack: Bool {
        heldBackForVpn(vpnOnly: vpnOnly, fingerprint: activeSummary?.fingerprint, vpnUp: VpnMonitor.shared.up)
    }

    /// Sets whether `context` is reached over a VPN only; the cluster on screen reloads (with
    /// the VPN off, the screens then say to connect it).
    func setVpnOnly(_ on: Bool, for context: ContextSummary) {
        guard !context.fingerprint.isEmpty else { return }
        storeVpnOnly(on ? vpnOnly.union([context.fingerprint]) : vpnOnly.subtracting([context.fingerprint]))
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

    // Internal, not private: AppModel+Clusters.swift uses it.

    func storeVpnOnly(_ fingerprints: Set<String>) {
        guard fingerprints != vpnOnly else { return }
        vpnOnly = fingerprints
        UserDefaults.standard.set(fingerprints.sorted(), forKey: Keys.vpnOnly)
    }

    /// Whether the background checks watch `context`'s cluster (off on any of its contexts: not).
    func watchesInBackground(_ context: ContextSummary) -> Bool {
        watchedInBackground(context, contexts: summary?.contexts ?? [], unwatched: monitorUnwatched)
    }

    /// Turns the background checks of `context`'s cluster on or off.
    func setWatchInBackground(_ on: Bool, for context: ContextSummary) {
        storeMonitorUnwatched(settingWatched(on, for: context, contexts: summary?.contexts ?? [], unwatched: monitorUnwatched))
    }

    /// Whether `context`'s cluster records actions (a Kubernetes Event per change).
    func recordsActions(_ context: ContextSummary) -> Bool {
        clusterFlagged(context, contexts: summary?.contexts ?? [], members: auditEventClusters)
    }

    /// Turns "Record actions in the cluster" on or off for `context`'s cluster (all its contexts).
    func setRecordActions(_ on: Bool, for context: ContextSummary) {
        storeAuditEventClusters(settingClusterFlag(on, for: context, contexts: summary?.contexts ?? [],
                                                   members: auditEventClusters))
    }

    /// Saves the clusters that record actions and hands them to Go.
    // Internal, not private: AppModel+Clusters.swift uses it.
    func storeAuditEventClusters(_ fingerprints: Set<String>) {
        guard fingerprints != auditEventClusters else { return }
        auditEventClusters = fingerprints
        UserDefaults.standard.set(fingerprints.sorted(), forKey: AuditEventKeys.clusters)
        TalosClient.setAuditEventClusters(fingerprints)
    }

    // Internal, not private: AppModel+Clusters.swift uses it.

    func storeMonitorUnwatched(_ fingerprints: Set<String>) {
        guard fingerprints != monitorUnwatched else { return }
        monitorUnwatched = fingerprints
        UserDefaults.standard.set(fingerprints.sorted(), forKey: BackgroundMonitor.unwatchedKey)
    }

    /// Tells the widget which clusters it can show, named as on screen, and which one is active
    /// (what a widget with no cluster chosen shows).
    // Internal, not private: AppModel+Clusters.swift uses it.
    func publishWidgetClusters() {
        // A widget set to a cluster opens that cluster's screen (a share link, routed after unlock).
        let clusters = summary.map { summary in
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
        SpotlightIndex.update(summary: summary, active: activeContext, labels: labels,
                              enabled: keepLastKnown && !lock.enabled && !privacyMask)
    }

    /// The VPN came up: a VPN-only cluster on screen reloads.
    // Internal, not private: AppModel.swift uses it.
    func reloadIfHeldBack() {
        if let shown = activeSummary?.fingerprint, vpnOnly.contains(shown) { dataGeneration += 1 }
    }

    /// Off: everything kept so far is deleted, with its key.
    func setKeepLastKnown(_ enabled: Bool) {
        keepLastKnown = enabled
        UserDefaults.standard.set(enabled, forKey: Keys.keepLastKnown)
        if !enabled { LastKnownStore.wipe() }
        updateSpotlight()
    }

    /// Turns the screenshot mode on or off, keeping its words (the Privacy toggle).
    func setPrivacyMask(_ on: Bool) async {
        await setPrivacyMask(on, words: privacyWords)
    }

    /// Turns the screenshot mode on or off (or changes its words). One switch at a time: a
    /// second call waits for the first, so the two never interleave.
    func setPrivacyMask(_ enabled: Bool, words: String) async {
        let previous = maskSwitch
        let current = Task {
            await previous?.value
            await switchPrivacyMask(enabled, words: words)
        }
        maskSwitch = current
        await current.value
    }

    /// With the placeholder up (maskTransition), drops the data held with the previous names
    /// (widget snapshot, last known state, live samples, search history), then changes Go's
    /// mask and re-reads the config, whose context names are masked too (MaskTransition).
    private func switchPrivacyMask(_ enabled: Bool, words: String) async {
        guard enabled != privacyMask || words != privacyWords else { return }
        // Words changed while the mode is off: nothing shown changes.
        guard enabled || privacyMask else {
            storePrivacyMask(enabled, words: words)
            return
        }
        await MaskTransition.run(
            begin: { maskTransition = true },
            dropData: { dropMaskedData() },
            applyMask: { storePrivacyMask(enabled, words: words) },
            reread: {
                await reparse()
                dataGeneration += 1
                // Lets the screens under the placeholder drop what they hold (their onChange and
                // .task(id:) on dataGeneration) before it goes, so not one frame shows old names.
                try? await Task.sleep(for: .milliseconds(150))
            },
            end: { maskTransition = false }
        )
        // Not awaited under the placeholder: the notification center may answer slowly.
        if enabled { Task { await BackgroundMonitor.removeDeliveredAlerts() } }
    }

    /// What was kept under the previous names, dropped before Go's mask changes.
    private func dropMaskedData() {
        // Also resets the alert diff, which would otherwise see every node renamed.
        SharedStore.clear()
        LastKnownStore.wipe()
        // Its poll stops recording: the overview starts a new one with the new generation.
        ClusterLiveStore.shared.forget()
        SearchRecents.clear()
        RecentTargetStore.shared.clear()
        forgetFeatures()
    }

    private func storePrivacyMask(_ enabled: Bool, words: String) {
        privacyMask = enabled
        privacyWords = words
        UserDefaults.standard.set(enabled, forKey: PrivacyKeys.enabled)
        UserDefaults.standard.set(words, forKey: PrivacyKeys.words)
        TalosClient.setPrivacyMask(enabled: enabled, extraWords: words)
        updateSpotlight()
    }
}
