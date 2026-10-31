import Foundation
import Observation
import IchorCore

/// App-wide state: the imported config and the active context. The preferences are
/// SettingsStore's.
@Observable
@MainActor
final class AppModel {
    /// The preferences; views read them from the environment, next to this model.
    let settings: SettingsStore
    /// Views read it from the environment, next to this model.
    let lockStore: LockStore
    /// The nodes' features and reachability; views read them from the environment, likewise.
    let nodes: NodesStore
    /// Each cluster's saved repository and pull requests; views read them from the environment, likewise.
    let git: ClusterGitStore

    // Internal, not private: AppModel+Clusters.swift uses it.
    enum Keys {
        static let context = "activeContext"
        /// Position of the active context in the config: unlike the name, it survives the
        /// screenshot mode (Go masks context names but keeps their order).
        static let contextIndex = "activeContextIndex"
        static let clusterColors = "clusterColors"
        static let clusterNames = "clusterNames"
        static let clusterTags = "clusterTags"
        static let kubeServers = "kubeServers"
        static let kubeAccess = "kubeAccess"
        static let snapshotKeys = "snapshotKeys"
        static let skippedTalosUpdates = "skippedTalosUpdates"
    }

    /// The stored talosconfig (Talos clusters), nil when none is.
    // Internal, not private(set): AppModel+Clusters.swift sets it.
    var yaml: String?
    /// The stored kubeconfig (clusters added without Talos), nil when none is.
    // Internal, not private(set): AppModel+Clusters.swift sets it.
    var kubeYAML: String?
    /// Every stored cluster: the talosconfig's, then the kubeconfig's.
    // Internal, not private(set): AppModel+Clusters.swift sets it.
    var summary: ConfigSummary?
    /// The two stores parsed apart, to combine again when one of them changes.
    // Internal, not private: AppModel+Clusters.swift uses it.
    @ObservationIgnored var talosSummary: ConfigSummary?
    // Internal, not private: AppModel+Clusters.swift uses it.
    @ObservationIgnored var kubeSummary: ConfigSummary?
    /// The stored configs that could not be read at load (decrypted or parsed): the app then
    /// shows neither and writes nothing until a retry reads them, or the user deletes them.
    // Internal, not private(set): AppModel+Clusters.swift sets it.
    var unreadable: [StoredConfigKind] = []
    // Internal, not private(set): AppModel+Clusters.swift sets it.
    var loaded = false

    var activeContext = "" {
        didSet {
            if activeContext != oldValue { nodes.forget() }
            UserDefaults.standard.set(activeContext, forKey: Keys.context)
            if let index = summary?.contexts.firstIndex(where: { $0.name == activeContext }) {
                UserDefaults.standard.set(index, forKey: Keys.contextIndex)
            }
            if activeContext != oldValue {
                publishWidgetClusters()
                updateSpotlight()
                cancelCalls(of: oldValue)
            }
        }
    }

    /// Ends the reads still under way on the cluster just left: they would keep threads and the
    /// network busy for up to 20 s. Off the main actor, though the Go call returns at once.
    private func cancelCalls(of context: String) {
        guard !context.isEmpty,
            let config = config(for: summary?.contexts.first(where: { $0.name == context }))
        else { return }
        Task { await offMainActor { TalosClient.cancelCalls(config: config, context: context) } }
    }

    /// Main color (0xRRGGBB) of each cluster, by context fingerprint (not its name, which the
    /// screenshot mode masks). The accent color is the one of the cluster on screen.
    private(set) var clusterColors: [String: Int]

    /// Names the user gave clusters, by context fingerprint, when the talosconfig context
    /// name is not a nice one. Only on this device: the talosconfig is left as it is, so
    /// importing it again still updates the same cluster.
    private(set) var clusterNames: [String: String]

    /// The tags the user gave clusters ("prod", "eu"), by context fingerprint. Only on this
    /// device and in backups, never in the config.
    private(set) var clusterTags: [String: [String]]

    /// The Kubernetes API address the user set for clusters, by context fingerprint, to use
    /// instead of the one in the cluster's kubeconfig (checked by TalosClient.normalizeKubeServer).
    private(set) var kubeServers: [String: String]

    /// The Kubernetes access of Talos clusters (K5): Talos fingerprint → fingerprint of the
    /// kubeconfig cluster their Kubernetes calls go through; absent: the admin kubeconfig
    /// Talos issues. Only on this device and in backups.
    private(set) var kubeAccess: [String: String]

    /// The public keys (age, SSH or YubiKey, one per line) each cluster's etcd snapshots were
    /// last encrypted for, by context fingerprint. Not secret; only on this device.
    private(set) var snapshotKeys: [String: String]

    /// The Talos release ("v1.14.2") each cluster's update card was skipped for, by context
    /// fingerprint: the card stays away until a newer one is out. Only on this device.
    private(set) var skippedTalosUpdates: [String: String]

    /// Bumped when the screenshot mode changes, so screens reload instead of showing old data.
    // Internal, not private(set): AppModel+Clusters.swift and SettingsStore.swift set it.
    var dataGeneration = 0

    /// Public IPs Talos does not know, found by a curl pod per node (PublicIPStore), by cluster
    /// fingerprint, and the clusters being probed: here, not in a view, since a probe takes
    /// minutes and outlives the screen that started it.
    // Internal, not private(set): AppModel+Clusters.swift sets it.
    var publicIPReports: [String: PublicIPReport] = PublicIPStore.read() ?? [:]
    // Internal, not private(set): AppModel+PublicIPs.swift sets it.
    var detectingPublicIPs: Set<String> = []

    init(settings: SettingsStore, lock: LockStore, nodes: NodesStore, git: ClusterGitStore) {
        self.settings = settings
        lockStore = lock
        self.nodes = nodes
        self.git = git
        clusterColors = UserDefaults.standard.dictionary(forKey: Keys.clusterColors) as? [String: Int] ?? [:]
        clusterNames = UserDefaults.standard.dictionary(forKey: Keys.clusterNames) as? [String: String] ?? [:]
        clusterTags = UserDefaults.standard.dictionary(forKey: Keys.clusterTags) as? [String: [String]] ?? [:]
        kubeServers = UserDefaults.standard.dictionary(forKey: Keys.kubeServers) as? [String: String] ?? [:]
        kubeAccess = UserDefaults.standard.dictionary(forKey: Keys.kubeAccess) as? [String: String] ?? [:]
        snapshotKeys = UserDefaults.standard.dictionary(forKey: Keys.snapshotKeys) as? [String: String] ?? [:]
        skippedTalosUpdates =
            UserDefaults.standard.dictionary(forKey: Keys.skippedTalosUpdates) as? [String: String] ?? [:]
        settings.model = self
        lockStore.model = self
        nodes.model = self
        VpnMonitor.shared.onConnect = { [weak self] in self?.reloadIfHeldBack() }
    }

    /// Nil while the cluster on screen waits for its VPN (see vpnHeldBack): its calls could only time out.
    var client: TalosClient? {
        guard !vpnHeldBack else { return nil }
        let kubeServer = activeSummary.flatMap { kubeServers[$0.fingerprint] } ?? ""
        let link = kubeLink(for: activeSummary)
        return config(for: activeSummary).map {
            TalosClient(
                config: $0, context: activeContext, kubeServer: kubeServer, kubeLink: link, kubeOnly: activeIsKube)
        }
    }

    var activeSummary: ContextSummary? { summary?.context(named: activeContext) }

    /// A cluster is stored (in either store).
    var hasConfig: Bool { yaml != nil || kubeYAML != nil }

    /// The cluster on screen was added from a kubeconfig: no Talos API.
    var activeIsKube: Bool { activeSummary?.isKube == true }

    /// The stored config the Go calls for `context` take: the kubeconfig for a cluster added from one.
    func config(for context: ContextSummary?) -> String? {
        StoredConfigs(talos: yaml, kube: kubeYAML).config(for: context)
    }

    /// A cluster's main color (0xRRGGBB); the default one while it has none.
    func seed(of context: ContextSummary?) -> Int {
        context.flatMap { clusterColors[$0.fingerprint] } ?? defaultClusterSeed
    }

    func setColor(_ rgb: Int, for context: ContextSummary) {
        guard !context.fingerprint.isEmpty else { return }
        storeColors(clusterColors.merging([context.fingerprint: rgb]) { _, new in new })
    }

    /// Names `context` `input`; a blank one goes back to its context name.
    func rename(_ context: ContextSummary, to input: String) {
        guard !context.fingerprint.isEmpty else { return }
        var names = clusterNames
        names[context.fingerprint] = normalizeClusterName(input)
        storeNames(names)
    }

    /// Gives `context`'s cluster (all its contexts, so a fleet row finds them whichever was
    /// checked) the tags typed in `input`.
    func setTags(_ input: String, for context: ContextSummary) {
        guard !context.fingerprint.isEmpty else { return }
        let tags = normalizeTags(input)
        var saved = clusterTags
        for fingerprint in clusterFingerprints(of: context, contexts: summary?.contexts ?? []) {
            saved[fingerprint] = tags.isEmpty ? nil : tags
        }
        storeTags(saved)
    }

    /// Sets the Kubernetes API address of `context` (`server` already normalized, "" for the kubeconfig's).
    func setKubeServer(_ server: String, for context: ContextSummary) {
        guard !context.fingerprint.isEmpty else { return }
        var servers = kubeServers
        servers[context.fingerprint] = server.nonEmpty
        storeKubeServers(servers)
    }

    /// Remembers the public keys the active cluster's snapshots are encrypted for ("" forgets them).
    func setSnapshotKeys(_ keys: String) {
        guard let fingerprint = activeSummary?.fingerprint, !fingerprint.isEmpty else { return }
        var saved = snapshotKeys
        let trimmed = keys.trimmingCharacters(in: .whitespacesAndNewlines)
        saved[fingerprint] = trimmed.nonEmpty
        storeSnapshotKeys(saved)
    }

    var activeSnapshotKeys: String { activeSummary.flatMap { snapshotKeys[$0.fingerprint] } ?? "" }

    // Internal, not private: AppModel+Clusters.swift uses it.

    func storeSnapshotKeys(_ keys: [String: String]) {
        guard keys != snapshotKeys else { return }
        snapshotKeys = keys
        UserDefaults.standard.set(keys, forKey: Keys.snapshotKeys)
    }

    /// Skips the Talos release `version` for the active cluster: its update card stays away
    /// until a newer one is out.
    func skipTalosUpdate(_ version: String) {
        guard let fingerprint = activeSummary?.fingerprint, !fingerprint.isEmpty, !version.isEmpty else { return }
        var saved = skippedTalosUpdates
        saved[fingerprint] = version
        storeSkippedTalosUpdates(saved)
    }

    var activeSkippedTalosUpdate: String? { activeSummary.flatMap { skippedTalosUpdates[$0.fingerprint] } }

    // Internal, not private: AppModel+Clusters.swift uses it.

    func storeSkippedTalosUpdates(_ versions: [String: String]) {
        guard versions != skippedTalosUpdates else { return }
        skippedTalosUpdates = versions
        UserDefaults.standard.set(versions, forKey: Keys.skippedTalosUpdates)
    }

    /// A sign-in, a sign-out or a new Kubernetes access changed what the cluster's calls can
    /// do: the screens reload.
    func reloadKubernetes() {
        dataGeneration += 1
    }

    /// Stores the Kubernetes access links (see kubeAccess).
    func storeKubeAccess(_ links: [String: String]) {
        guard links != kubeAccess else { return }
        kubeAccess = links
        UserDefaults.standard.set(links, forKey: Keys.kubeAccess)
    }

    // Internal, not private: AppModel+Clusters.swift uses it.

    func storeKubeServers(_ servers: [String: String]) {
        guard servers != kubeServers else { return }
        kubeServers = servers
        UserDefaults.standard.set(servers, forKey: Keys.kubeServers)
    }

    // Internal, not private: AppModel+Clusters.swift uses it.

    func storeTags(_ tags: [String: [String]]) {
        guard tags != clusterTags else { return }
        clusterTags = tags
        UserDefaults.standard.set(tags, forKey: Keys.clusterTags)
    }

    // Internal, not private: AppModel+Clusters.swift uses it.

    func storeNames(_ names: [String: String]) {
        guard names != clusterNames else { return }
        clusterNames = names
        UserDefaults.standard.set(names, forKey: Keys.clusterNames)
        QuickActions.update(summary: summary, labels: labels)
        publishWidgetClusters()
        updateSpotlight()
    }

    /// Shows the cluster a quick action stands for; false when it is no longer imported.
    func selectCluster(fingerprint: String) -> Bool {
        guard let context = summary?.contexts.first(where: { $0.fingerprint == fingerprint }) else { return false }
        activeContext = context.name
        return true
    }

    // Internal, not private: AppModel+Clusters.swift uses it.

    func storeColors(_ colors: [String: Int]) {
        guard colors != clusterColors else { return }
        clusterColors = colors
        UserDefaults.standard.set(colors, forKey: Keys.clusterColors)
    }

    /// Shows the cluster `step` positions after the active one (before it when negative).
    func selectAdjacentCluster(step: Int) {
        if let next = summary?.adjacentContext(to: activeContext, step: step) { activeContext = next }
    }

    /// Privileged actions are only shown when the imported config's role allows them.
    func allows(_ feature: Feature) -> Bool {
        activeSummary?.allows(feature, kubeLinked: kubeLink(for: activeSummary) != nil) ?? false
    }

}
