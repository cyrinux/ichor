import Foundation
import Observation
import IchorCore

enum ThemeMode: String, CaseIterable, Identifiable {
    case auto, light, dark, black

    var id: String { rawValue }
    var label: String {
        switch self {
        case .auto: String(localized: "Auto")
        case .light: String(localized: "Light")
        case .dark: String(localized: "Dark")
        case .black: String(localized: "Black")
        }
    }
}

/// App-wide state: the imported config, the active context, preferences and the lock.
@Observable
@MainActor
final class AppModel {
    // Internal, not private: AppModel+Clusters.swift uses it.
    enum Keys {
        static let context = "activeContext"
        /// Position of the active context in the config: unlike the name, it survives the
        /// screenshot mode (Go masks context names but keeps their order).
        static let contextIndex = "activeContextIndex"
        static let theme = "themeMode"
        static let lock = "appLockEnabled"
        static let clusterColors = "clusterColors"
        static let clusterNames = "clusterNames"
        static let kubeServers = "kubeServers"
        static let kubeAccess = "kubeAccess"
        static let snapshotKeys = "snapshotKeys"
        static let skippedTalosUpdates = "skippedTalosUpdates"
        static let keepLastKnown = "keepLastKnownState"
        static let vpnOnly = "vpnOnlyClusters"
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
    // Internal, not private(set): AppModel+SignIn.swift sets it.
    var lock: AppLockState
    /// The enrolled security keys (SecurityKeyStore), nil when none; see IchorCore/SecurityKeys.swift.
    // Internal, not private(set): AppModel+SignIn.swift sets it.
    var securityKeys: SecurityKeyEnrolment? = SecurityKeyStore.current
    /// Whether a tap of a security key is needed to read the stored configs (Face ID alone is refused).
    var requiresKey: Bool { securityKeys?.required == true }

    var activeContext = "" {
        didSet {
            if activeContext != oldValue { forgetFeatures() }
            UserDefaults.standard.set(activeContext, forKey: Keys.context)
            if let index = summary?.contexts.firstIndex(where: { $0.name == activeContext }) {
                UserDefaults.standard.set(index, forKey: Keys.contextIndex)
            }
            if activeContext != oldValue {
                publishWidgetClusters()
                updateSpotlight()
            }
        }
    }

    /// Main color (0xRRGGBB) of each cluster, by context fingerprint (not its name, which the
    /// screenshot mode masks). The accent color is the one of the cluster on screen.
    private(set) var clusterColors: [String: Int]

    /// Names the user gave clusters, by context fingerprint, when the talosconfig context
    /// name is not a nice one. Only on this device: the talosconfig is left as it is, so
    /// importing it again still updates the same cluster.
    private(set) var clusterNames: [String: String]

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

    /// The clusters reached over a VPN only, by context fingerprint: without a VPN up the app
    /// does not try them (screens say to connect it, background checks wait). Only on this device.
    // Internal, not private(set): AppModel+Settings.swift sets it.
    var vpnOnly: Set<String>

    /// The Git repository saved for each cluster (its Talos and Kubernetes config), by context
    /// fingerprint; no token in it. Only on this device, and in backups.
    // Internal, not private(set): AppModel+Settings.swift sets it.
    var clusterRepos: [String: ClusterRepo]

    /// The pull requests opened from the phone, per cluster (context fingerprint), newest first;
    /// no token in them. Only on this device.
    // Internal, not private(set): AppModel+Settings.swift sets it.
    var proposals: [String: [GitProposalRecord]]

    /// The clusters turned off with "Watch in the background", by context fingerprint: the
    /// background checks skip them (on by default). Only on this device.
    // Internal, not private(set): AppModel+Settings.swift sets it.
    var monitorUnwatched: Set<String>

    /// The clusters that record actions ("Record actions in the cluster": a Kubernetes Event on
    /// each object the app changes there), by context fingerprint; off by default.
    // Internal, not private(set): AppModel+Settings.swift sets it.
    var auditEventClusters: Set<String>

    var theme: ThemeMode {
        didSet { UserDefaults.standard.set(theme.rawValue, forKey: Keys.theme) }
    }

    /// Screenshot mode (masking done in Go, see TalosClient.setPrivacyMask) and its extra words.
    // Internal, not private(set): AppModel+Settings.swift sets it.
    var privacyMask: Bool
    // Internal, not private(set): AppModel+Settings.swift sets it.
    var privacyWords: String
    /// Bumped when the screenshot mode changes, so screens reload instead of showing old data.
    // Internal, not private(set): AppModel+Clusters.swift sets it.
    var dataGeneration = 0
    /// True while the screenshot mode is being turned on or off: the app shows a placeholder
    /// instead of its screens, so no name shows under the wrong setting (see setPrivacyMask).
    // Internal, not private(set): AppModel+Settings.swift sets it.
    var maskTransition = false
    /// The switch of the screenshot mode in progress: the next one waits for it.
    // Internal, not private: AppModel+Settings.swift uses it.
    @ObservationIgnored var maskSwitch: Task<Void, Never>?

    /// Keep the last data fetched from each cluster on the phone (LastKnownStore), to show it
    /// when the cluster cannot be reached. Off by default.
    // Internal, not private(set): AppModel+Settings.swift sets it.
    var keepLastKnown: Bool

    /// What each node's Talos version can do (Go NodeFeatures), by node address; loaded once
    /// per node and Talos version, see loadFeatures.
    private(set) var nodeFeatures: [String: NodeFeatures] = [:]

    /// The nodes found throttled when their Hardware screen last read their sensors (by
    /// address): the overview never reads sensors itself, so a row says nothing until then.
    private(set) var throttledNodes: Set<String> = []

    /// Public IPs Talos does not know, found by a curl pod per node (PublicIPStore), by cluster
    /// fingerprint, and the clusters being probed: here, not in a view, since a probe takes
    /// minutes and outlives the screen that started it.
    // Internal, not private(set): AppModel+Clusters.swift sets it.
    var publicIPReports: [String: PublicIPReport] = PublicIPStore.read() ?? [:]
    private(set) var detectingPublicIPs: Set<String> = []
    @ObservationIgnored private var featuresGeneration = 0

    init() {
        theme = ThemeMode(rawValue: UserDefaults.standard.string(forKey: Keys.theme) ?? "") ?? .auto
        privacyMask = UserDefaults.standard.bool(forKey: PrivacyKeys.enabled)
        privacyWords = UserDefaults.standard.string(forKey: PrivacyKeys.words) ?? ""
        lock = AppLockState(enabled: UserDefaults.standard.bool(forKey: Keys.lock))
        // On unless the user turned it off: the home opens on it at once.
        keepLastKnown = UserDefaults.standard.object(forKey: Keys.keepLastKnown) as? Bool ?? true
        clusterColors = UserDefaults.standard.dictionary(forKey: Keys.clusterColors) as? [String: Int] ?? [:]
        clusterNames = UserDefaults.standard.dictionary(forKey: Keys.clusterNames) as? [String: String] ?? [:]
        kubeServers = UserDefaults.standard.dictionary(forKey: Keys.kubeServers) as? [String: String] ?? [:]
        kubeAccess = UserDefaults.standard.dictionary(forKey: Keys.kubeAccess) as? [String: String] ?? [:]
        snapshotKeys = UserDefaults.standard.dictionary(forKey: Keys.snapshotKeys) as? [String: String] ?? [:]
        skippedTalosUpdates =
            UserDefaults.standard.dictionary(forKey: Keys.skippedTalosUpdates) as? [String: String] ?? [:]
        vpnOnly = Set(UserDefaults.standard.stringArray(forKey: Keys.vpnOnly) ?? [])
        clusterRepos = ClusterRepoStore.decode(UserDefaults.standard.data(forKey: ClusterRepoStore.key))
        proposals = ProposalsStore.decode(UserDefaults.standard.data(forKey: ProposalsStore.key))
        monitorUnwatched = Set(UserDefaults.standard.stringArray(forKey: BackgroundMonitor.unwatchedKey) ?? [])
        auditEventClusters = Set(UserDefaults.standard.stringArray(forKey: AuditEventKeys.clusters) ?? [])
        VpnMonitor.shared.onConnect = { [weak self] in self?.reloadIfHeldBack() }
        // The background monitor saved the proposals' states (merged, closed).
        NotificationCenter.default.addObserver(forName: .proposalsChanged, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.reloadProposals() }
        }
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

    /// How clusters are called on screen (given names, unless the screenshot mode is on).
    var labels: ClusterLabels { ClusterLabels(names: clusterNames, masked: privacyMask) }

    /// The name of the cluster on screen.
    var activeLabel: String { activeSummary.map(labels.of) ?? activeContext }

    /// Names `context` `input`; a blank one goes back to its context name.
    func rename(_ context: ContextSummary, to input: String) {
        guard !context.fingerprint.isEmpty else { return }
        var names = clusterNames
        names[context.fingerprint] = normalizeClusterName(input)
        storeNames(names)
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

    /// The active cluster's public IP probe; none in screenshot mode (kept unmasked).
    var activePublicIPs: PublicIPReport? {
        privacyMask ? nil : activeSummary.flatMap { publicIPReports[$0.fingerprint] }
    }

    var isDetectingPublicIPs: Bool { activeSummary.map { detectingPublicIPs.contains($0.fingerprint) } ?? false }

    /// Probes the active cluster's public IPs (see TalosClient.detectPublicIPs) and keeps the
    /// answer, unless screenshot mode came on meanwhile: Go then returned placeholders. Nil when
    /// a probe of this cluster is already running.
    func detectPublicIPs() async throws -> PublicIPReport? {
        guard let client, let fingerprint = activeSummary?.fingerprint, !fingerprint.isEmpty,
            !detectingPublicIPs.contains(fingerprint)
        else { return nil }
        detectingPublicIPs.insert(fingerprint)
        defer { detectingPublicIPs.remove(fingerprint) }
        let report = try await client.detectPublicIPs()
        guard !privacyMask, summary?.contexts.contains(where: { $0.fingerprint == fingerprint }) == true else {
            return report
        }
        publicIPReports[fingerprint] = report
        try? PublicIPStore.save(publicIPReports)
        return report
    }

    /// Whether node's Talos version has `feature`; supported while its features are unknown.
    func support(_ feature: NodeFeature, node: String) -> FeatureSupport {
        featureSupport(nodeFeatures[node], feature)
    }

    /// Support of a cluster-wide feature across the nodes whose features are known.
    func clusterSupport(_ feature: NodeFeature) -> FeatureSupport {
        IchorCore.clusterSupport(Array(nodeFeatures.values), feature)
    }

    /// Loads node's features unless they are cached for `version` (the node's Talos version
    /// when the caller knows it: an upgraded node is asked again). Failures are silent: an
    /// unknown node counts as supporting everything and answers for itself.
    func loadFeatures(node: String, version: String = "") async {
        if let cached = nodeFeatures[node],
            version.isEmpty || cached.version.isEmpty
                || compareTalosVersions(cached.version, version) == 0
        {
            return
        }
        guard let client else { return }
        let generation = featuresGeneration
        guard let loaded = try? await client.features(node: node), generation == featuresGeneration else { return }
        nodeFeatures[node] = loaded
    }

    /// Records what node's sensors said, for its row in the node lists.
    func setThrottled(node: String, _ throttled: Bool) {
        if throttled { throttledNodes.insert(node) } else { throttledNodes.remove(node) }
    }

    /// Drops node's cached features and asks again: its Talos version just changed (upgrade).
    func reloadFeatures(node: String) async {
        nodeFeatures[node] = nil
        await loadFeatures(node: node)
    }

    /// The features of every reachable node of an overview, in parallel.
    /// Also records their MACs for Wake-on-LAN (recordNodeMacs), alongside.
    func loadFeatures(of nodes: [NodeOverview]) async {
        Task { await recordNodeMacs(of: nodes) }
        await withTaskGroup(of: Void.self) { group in
            for node in nodes where node.reachable {
                group.addTask { await self.loadFeatures(node: node.node, version: node.version) }
            }
        }
    }

    /// Other context, other config or other (masked) node names: the cache no longer applies.
    // Internal, not private: AppModel+Clusters.swift uses it.
    func forgetFeatures() {
        featuresGeneration += 1
        nodeFeatures = [:]
        throttledNodes = []
    }

    /// Discovered members set aside with "Not now", per context, until the app restarts.
    @ObservationIgnored private var dismissedByContext: [String: Set<String>] = [:]

    func dismissedNodes(of context: String) -> Set<String> { dismissedByContext[context] ?? [] }

    func dismissNodes(_ nodes: [DiscoveredNode], of context: String) {
        dismissedByContext[context, default: []].formUnion(nodes.map(\.address))
    }

}
