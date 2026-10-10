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
    private enum Keys {
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
    private(set) var yaml: String?
    /// The stored kubeconfig (clusters added without Talos), nil when none is.
    private(set) var kubeYAML: String?
    /// Every stored cluster: the talosconfig's, then the kubeconfig's.
    private(set) var summary: ConfigSummary?
    /// The two stores parsed apart, to combine again when one of them changes.
    @ObservationIgnored private var talosSummary: ConfigSummary?
    @ObservationIgnored private var kubeSummary: ConfigSummary?
    /// The stored configs that could not be read at load (decrypted or parsed): the app then
    /// shows neither and writes nothing until a retry reads them, or the user deletes them.
    private(set) var unreadable: [StoredConfigKind] = []
    private(set) var loaded = false
    private(set) var lock: AppLockState
    /// The enrolled security keys (SecurityKeyStore), nil when none; see IchorCore/SecurityKeys.swift.
    private(set) var securityKeys: SecurityKeyEnrolment? = SecurityKeyStore.current
    /// Whether a tap of a security key is needed to read the stored configs (Face ID alone is refused).
    var requiresKey: Bool { securityKeys?.required == true }

    var activeContext = "" {
        didSet {
            if activeContext != oldValue { forgetFeatures() }
            UserDefaults.standard.set(activeContext, forKey: Keys.context)
            if let index = summary?.contexts.firstIndex(where: { $0.name == activeContext }) {
                UserDefaults.standard.set(index, forKey: Keys.contextIndex)
            }
            if activeContext != oldValue { publishWidgetClusters() }
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
    /// instead of the one in the kubeconfig Talos issues (checked by TalosClient.normalizeKubeServer).
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
    private(set) var vpnOnly: Set<String>

    /// The clusters turned off with "Watch in the background", by context fingerprint: the
    /// background checks skip them (on by default). Only on this device.
    private(set) var monitorUnwatched: Set<String>

    var theme: ThemeMode {
        didSet { UserDefaults.standard.set(theme.rawValue, forKey: Keys.theme) }
    }

    /// Screenshot mode (masking done in Go, see TalosClient.setPrivacyMask) and its extra words.
    private(set) var privacyMask: Bool
    private(set) var privacyWords: String
    /// Bumped when the screenshot mode changes, so screens reload instead of showing old data.
    private(set) var dataGeneration = 0

    /// Keep the last data fetched from each cluster on the phone (LastKnownStore), to show it
    /// when the cluster cannot be reached. Off by default.
    private(set) var keepLastKnown: Bool

    /// What each node's Talos version can do (Go NodeFeatures), by node address; loaded once
    /// per node and Talos version, see loadFeatures.
    private(set) var nodeFeatures: [String: NodeFeatures] = [:]

    /// The nodes found throttled when their Hardware screen last read their sensors (by
    /// address): the overview never reads sensors itself, so a row says nothing until then.
    private(set) var throttledNodes: Set<String> = []

    /// Public IPs Talos does not know, found by a curl pod per node (PublicIPStore), by cluster
    /// fingerprint, and the clusters being probed: here, not in a view, since a probe takes
    /// minutes and outlives the screen that started it.
    private(set) var publicIPReports: [String: PublicIPReport] = PublicIPStore.read() ?? [:]
    private(set) var detectingPublicIPs: Set<String> = []
    @ObservationIgnored private var featuresGeneration = 0

    init() {
        theme = ThemeMode(rawValue: UserDefaults.standard.string(forKey: Keys.theme) ?? "") ?? .auto
        privacyMask = UserDefaults.standard.bool(forKey: PrivacyKeys.enabled)
        privacyWords = UserDefaults.standard.string(forKey: PrivacyKeys.words) ?? ""
        lock = AppLockState(enabled: UserDefaults.standard.bool(forKey: Keys.lock))
        keepLastKnown = UserDefaults.standard.bool(forKey: Keys.keepLastKnown)
        clusterColors = UserDefaults.standard.dictionary(forKey: Keys.clusterColors) as? [String: Int] ?? [:]
        clusterNames = UserDefaults.standard.dictionary(forKey: Keys.clusterNames) as? [String: String] ?? [:]
        kubeServers = UserDefaults.standard.dictionary(forKey: Keys.kubeServers) as? [String: String] ?? [:]
        kubeAccess = UserDefaults.standard.dictionary(forKey: Keys.kubeAccess) as? [String: String] ?? [:]
        snapshotKeys = UserDefaults.standard.dictionary(forKey: Keys.snapshotKeys) as? [String: String] ?? [:]
        skippedTalosUpdates = UserDefaults.standard.dictionary(forKey: Keys.skippedTalosUpdates) as? [String: String] ?? [:]
        vpnOnly = Set(UserDefaults.standard.stringArray(forKey: Keys.vpnOnly) ?? [])
        monitorUnwatched = Set(UserDefaults.standard.stringArray(forKey: BackgroundMonitor.unwatchedKey) ?? [])
        VpnMonitor.shared.onConnect = { [weak self] in self?.reloadIfHeldBack() }
    }

    /// Nil while the cluster on screen waits for its VPN (see vpnHeldBack): its calls could only time out.
    var client: TalosClient? {
        guard !vpnHeldBack else { return nil }
        let kubeServer = activeSummary.flatMap { kubeServers[$0.fingerprint] } ?? ""
        let link = kubeLink(for: activeSummary)
        return config(for: activeSummary).map { TalosClient(config: $0, context: activeContext, kubeServer: kubeServer, kubeLink: link) }
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

    private func storeSnapshotKeys(_ keys: [String: String]) {
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

    private func storeSkippedTalosUpdates(_ versions: [String: String]) {
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

    private func storeKubeServers(_ servers: [String: String]) {
        guard servers != kubeServers else { return }
        kubeServers = servers
        UserDefaults.standard.set(servers, forKey: Keys.kubeServers)
    }

    private func storeNames(_ names: [String: String]) {
        guard names != clusterNames else { return }
        clusterNames = names
        UserDefaults.standard.set(names, forKey: Keys.clusterNames)
        QuickActions.update(summary: summary, labels: labels)
        publishWidgetClusters()
    }

    /// Shows the cluster a quick action stands for; false when it is no longer imported.
    func selectCluster(fingerprint: String) -> Bool {
        guard let context = summary?.contexts.first(where: { $0.fingerprint == fingerprint }) else { return false }
        activeContext = context.name
        return true
    }

    private func storeColors(_ colors: [String: Int]) {
        guard colors != clusterColors else { return }
        clusterColors = colors
        UserDefaults.standard.set(colors, forKey: Keys.clusterColors)
    }

    /// Shows the cluster `step` positions after the active one (before it when negative).
    func selectAdjacentCluster(step: Int) {
        if let next = summary?.adjacentContext(to: activeContext, step: step) { activeContext = next }
    }

    /// Privileged actions are only shown when the imported config's role allows them.
    func allows(_ feature: Feature) -> Bool { activeSummary?.allows(feature, kubeLinked: kubeLink(for: activeSummary) != nil) ?? false }

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
              !detectingPublicIPs.contains(fingerprint) else { return nil }
        detectingPublicIPs.insert(fingerprint)
        defer { detectingPublicIPs.remove(fingerprint) }
        let report = try await client.detectPublicIPs()
        guard !privacyMask, summary?.contexts.contains(where: { $0.fingerprint == fingerprint }) == true else { return report }
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
        if let cached = nodeFeatures[node], version.isEmpty || cached.version.isEmpty
            || compareTalosVersions(cached.version, version) == 0 { return }
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
    private func forgetFeatures() {
        featuresGeneration += 1
        nodeFeatures = [:]
        throttledNodes = []
    }

    /// Loads the stored configs (Keychain); nothing is read before the first unlock.
    func load() async {
        defer { loaded = true }
        WakeOnLanStore.shared.loadIfNeeded()
        let stored = StoredConfigs.load()
        let parsed = await stored.parsed()
        // Fail closed: one store unreadable shows neither (the other alone could overwrite it).
        unreadable = parsed.unreadable
        guard unreadable.isEmpty, let all = ConfigSummary.combined(talos: parsed.talos, kube: parsed.kube) else { return }
        apply(talos: parsed.talos == nil ? nil : stored.talos, talosSummary: parsed.talos,
              kube: parsed.kube == nil ? nil : stored.kube, kubeSummary: parsed.kube,
              preferred: all.selectedContext(index: Self.savedContextIndex, name: UserDefaults.standard.string(forKey: Keys.context)))
    }

    /// Stores `newYAML`. With a config already stored its contexts are added to it (one
    /// stored talosconfig, a context per cluster). A stored context is never overwritten: one
    /// of the same name (Talos or kubeconfig cluster) is added as name-1, name-2…, unless
    /// `replacingSameCluster` (the demo added again) and it is the same cluster (same CA). The
    /// imported config's current context becomes the one shown.
    func save(yaml newYAML: String, replacingSameCluster: Bool = false) async throws {
        try checkWritable()
        let merged: String
        if yaml != nil || kubeYAML != nil {
            let stored = yaml ?? "", kube = kubeYAML ?? ""
            var choices = ""
            if replacingSameCluster {
                let conflicts = try await TalosClient.talosImportConflicts(stored: stored, kube: kube, added: newYAML)
                choices = "[" + conflicts.filter { $0.sameAs != nil }
                    .map { "{\"index\":\($0.index),\"replace\":true}" }
                    .joined(separator: ",") + "]"
            }
            merged = try await TalosClient.mergeTalosconfig(stored: stored, kube: kube, added: newYAML, choices: choices)
        } else {
            merged = newYAML
        }
        let parsed = try await TalosClient.parse(merged)
        try SecureConfigStore.save(Data(merged.utf8))
        apply(talos: merged, talosSummary: parsed, kube: kubeYAML, kubeSummary: kubeSummary, preferred: parsed.current)
    }

    /// Adds the contexts of the kubeconfig `added` to the stored kubeconfig, as `choices` say
    /// (contexts left out, same cluster replaced; see KubeImportConflicts for the names). The
    /// imported kubeconfig's current context becomes the one shown.
    func saveKube(added: String, choices: [KubeImportChoice]) async throws {
        try checkWritable()
        let merged = try await TalosClient.mergeKubeconfig(stored: kubeYAML ?? "", talos: yaml ?? "", added: added, choices: choices)
        let parsed = try await TalosClient.parseKubeconfig(merged)
        try SecureConfigStore.save(Data(merged.utf8), item: .kubeconfig)
        apply(talos: yaml, talosSummary: talosSummary, kube: merged, kubeSummary: parsed, preferred: parsed.current)
    }

    /// Adds the built-in Kubernetes demo next to the stored clusters, replacing one added before.
    func saveKubeDemo() async throws {
        let added = try await TalosClient.demoKubeconfig()
        let conflicts = try await TalosClient.kubeImportConflicts(stored: kubeYAML ?? "", talos: yaml ?? "", added: added)
        let choices = conflicts.filter { $0.sameAs != nil }.map { KubeImportChoice(index: $0.index, replace: true) }
        try await saveKube(added: added, choices: choices)
    }

    /// Replaces both stored configs with those of a restored backup (nil: none of that kind)
    /// and shows the context at `activeIndex`. Both are checked before anything is written, and
    /// a failed write puts the previous configs back, so a failure leaves everything as it was.
    func replace(talos newTalos: String?, kube newKube: String?, activeIndex: Int?) async throws {
        try checkWritable()
        var parsedTalos: ConfigSummary?
        var parsedKube: ConfigSummary?
        if let newTalos { parsedTalos = try await TalosClient.parse(newTalos) }
        if let newKube { parsedKube = try await TalosClient.parseKubeconfig(newKube) }
        guard let all = ConfigSummary.combined(talos: parsedTalos, kube: parsedKube) else {
            throw TalosError(message: String(localized: "The backup holds no cluster."))
        }
        let previous = StoredConfigs.load()
        do {
            try write(newTalos, item: .talosconfig)
            try write(newKube, item: .kubeconfig)
        } catch {
            try? write(previous.talos, item: .talosconfig)
            try? write(previous.kube, item: .kubeconfig)
            throw error
        }
        SharedStore.clear() // the widget stops showing the previous config's clusters
        apply(talos: newTalos, talosSummary: parsedTalos, kube: newKube, kubeSummary: parsedKube,
              preferred: all.selectedContext(index: activeIndex, name: nil))
        dataGeneration += 1
    }

    /// Writes are refused while a stored config could not be read (see unreadableConfigs).
    private func checkWritable() throws {
        guard unreadable.isEmpty else {
            throw TalosError(message: String(localized: "A stored config could not be read. Nothing is changed until it can be."))
        }
    }

    /// Stores `text` as `item`, or deletes it when nil.
    private func write(_ text: String?, item: SecureConfigStore.Item) throws {
        if let text {
            try SecureConfigStore.save(Data(text.utf8), item: item)
        } else {
            SecureConfigStore.delete(item)
        }
    }

    /// Restored names (all of them) and colors (the others keep the one just assigned) of the stored clusters.
    func restoreClusterSettings(names: [String: String], colors: [String: Int], kubeServers servers: [String: String],
                                vpnOnly restoredVpnOnly: Set<String> = [], kubeAccess links: [String: String] = [:]) {
        storeColors(clusterColors.merging(colors) { _, new in new })
        storeNames(names)
        storeKubeServers(servers)
        storeVpnOnly(restoredVpnOnly)
        storeKubeAccess(links)
    }

    /// Removes the cluster `name` (a context and its credentials) from the store it is in,
    /// showing its neighbour if it was the active one. Removing a store's last cluster deletes
    /// that store; removing the very last one deletes everything (clear).
    func removeCluster(_ name: String) async throws {
        try checkWritable()
        guard let before = summary, let removed = before.contexts.firstIndex(where: { $0.name == name }) else { return }
        guard before.contexts.count > 1 else {
            clear()
            return
        }
        let active = before.contexts.firstIndex { $0.name == activeContext } ?? removed
        // By position: the masked names of the screenshot mode may change with the set of contexts.
        let index = ConfigSummary.activeIndexAfterRemoval(active: active, removed: removed, remaining: before.contexts.count - 1)
        if before.contexts[removed].isKube {
            guard let current = kubeYAML else { return }
            let left = try await TalosClient.removeKubeContext(stored: current, context: name)
            // "" once no kubeconfig cluster is left: the store goes.
            let remaining = left.isEmpty ? nil : left
            var parsed: ConfigSummary?
            if let remaining { parsed = try await TalosClient.parseKubeconfig(remaining) }
            try write(remaining, item: .kubeconfig)
            applyIndex(talos: yaml, talosSummary: talosSummary, kube: remaining, kubeSummary: parsed, index: index)
        } else {
            guard let current = yaml else { return }
            // The talosconfig's last cluster: the store goes (kubeconfig clusters are left).
            var remaining: String?
            var parsed: ConfigSummary?
            if (talosSummary?.contexts.count ?? 0) > 1 {
                let left = try await TalosClient.removeContext(stored: current, context: name)
                remaining = left
                parsed = try await TalosClient.parse(left)
            }
            try write(remaining, item: .talosconfig)
            applyIndex(talos: remaining, talosSummary: parsed, kube: kubeYAML, kubeSummary: kubeSummary, index: index)
        }
    }

    /// apply, showing the context at `index` of the combined list.
    private func applyIndex(talos: String?, talosSummary: ConfigSummary?, kube: String?, kubeSummary: ConfigSummary?, index: Int?) {
        let all = ConfigSummary.combined(talos: talosSummary, kube: kubeSummary)
        apply(talos: talos, talosSummary: talosSummary, kube: kube, kubeSummary: kubeSummary,
              preferred: all?.selectedContext(index: index, name: nil))
    }

    /// Replaces the active context's ca/crt/key with those of `generated` (a renewed
    /// single-context talosconfig), keeping the other contexts and the active context. The
    /// result must parse, keep the same contexts and carry exactly the new certificate.
    func renewCredentials(with generated: String) async throws {
        try checkWritable()
        guard let current = yaml, let before = talosSummary else { throw TalosError(message: String(localized: "No talosconfig is stored.")) }
        let context = activeContext
        let patched = try await TalosClient.replaceContextCredentials(stored: current, generated: generated, context: context)
        let parsed = try await TalosClient.parse(patched)
        let issued = try await TalosClient.parse(generated)
        guard let renewed = parsed.context(named: context), let fresh = issued.context(named: context),
              renewed.certNotAfter == fresh.certNotAfter, Set(renewed.roles) == Set(fresh.roles),
              parsed.contexts.map(\.name) == before.contexts.map(\.name) else {
            throw TalosError(message: String(localized: "The renewed talosconfig did not validate; the stored one is unchanged."))
        }
        try SecureConfigStore.save(Data(patched.utf8))
        apply(talos: patched, talosSummary: parsed, kube: kubeYAML, kubeSummary: kubeSummary, preferred: context)
    }

    /// Adds discovered `nodes` (addresses) to the active context's nodes, keeping the other
    /// contexts; the overview reloads with them.
    func addNodes(_ nodes: [String]) async throws {
        try checkWritable()
        guard let current = yaml else { throw TalosError(message: String(localized: "No talosconfig is stored.")) }
        let context = activeContext
        let updated = try await TalosClient.addContextNodes(stored: current, context: context, nodes: nodes)
        let parsed = try await TalosClient.parse(updated)
        try SecureConfigStore.save(Data(updated.utf8))
        apply(talos: updated, talosSummary: parsed, kube: kubeYAML, kubeSummary: kubeSummary, preferred: context)
        dataGeneration += 1
    }

    /// Replaces the endpoints of the context `name` (the addresses the app connects through),
    /// keeping the other contexts; screens reload with them.
    func setEndpoints(_ endpoints: [String], of name: String) async throws {
        guard let current = yaml else { throw TalosError(message: String(localized: "No talosconfig is stored.")) }
        let updated = try await TalosClient.setContextEndpoints(stored: current, context: name, endpoints: endpoints)
        try await store(updated)
    }

    /// Puts each endpoint a network search found first among the endpoints of the contexts it
    /// answered for, all in one write: nothing is stored if one fails. Once a cluster answers
    /// again, node discovery offers the members it still misses.
    func addFoundEndpoints(_ matches: [EndpointMatch]) async throws {
        guard var updated = yaml else { throw TalosError(message: String(localized: "No talosconfig is stored.")) }
        for match in matches {
            for context in match.contexts {
                updated = try await TalosClient.addContextEndpoint(stored: updated, context: context, endpoint: match.endpoint)
            }
        }
        try await store(updated)
    }

    /// Stores an edited config, keeping the context on screen, and reloads the screens.
    private func store(_ updated: String) async throws {
        try checkWritable()
        let parsed = try await TalosClient.parse(updated)
        try SecureConfigStore.save(Data(updated.utf8))
        let index = summary?.contexts.firstIndex { $0.name == activeContext }
        let all = ConfigSummary.combined(talos: parsed, kube: kubeSummary)
        apply(talos: updated, talosSummary: parsed, kube: kubeYAML, kubeSummary: kubeSummary,
              preferred: all?.selectedContext(index: index, name: activeContext))
        dataGeneration += 1
    }

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

    private func storeVpnOnly(_ fingerprints: Set<String>) {
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

    private func storeMonitorUnwatched(_ fingerprints: Set<String>) {
        guard fingerprints != monitorUnwatched else { return }
        monitorUnwatched = fingerprints
        UserDefaults.standard.set(fingerprints.sorted(), forKey: BackgroundMonitor.unwatchedKey)
    }

    /// Tells the widget which clusters it can show, named as on screen, and which one is active
    /// (what a widget with no cluster chosen shows).
    private func publishWidgetClusters() {
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

    /// The VPN came up: a VPN-only cluster on screen reloads.
    private func reloadIfHeldBack() {
        if let shown = activeSummary?.fingerprint, vpnOnly.contains(shown) { dataGeneration += 1 }
    }

    /// Discovered members set aside with "Not now", per context, until the app restarts.
    @ObservationIgnored private var dismissedByContext: [String: Set<String>] = [:]

    func dismissedNodes(of context: String) -> Set<String> { dismissedByContext[context] ?? [] }

    func dismissNodes(_ nodes: [DiscoveredNode], of context: String) {
        dismissedByContext[context, default: []].formUnion(nodes.map(\.address))
    }

    /// Deletes every stored cluster: both configs, and what was kept about them.
    func clear() {
        SecureConfigStore.delete()
        LastKnownStore.wipe()
        PublicIPStore.wipe()
        MetricsStore.keep(fingerprints: []) // their credentials go with the config
        AlertmanagerStore.keep(fingerprints: [])
        WakeOnLanStore.shared.wipe()
        KubeAuthStore.shared.wipe() // its sealed item went with the others
        storeVpnOnly([])
        storeMonitorUnwatched([])
        AlertSnoozeStore.keep(clusters: [])
        storeKubeAccess([:])
        publicIPReports = [:]
        SharedStore.clear() // the widget stops showing the old clusters
        HistoryStore.wipe()
        forgetFeatures()
        yaml = nil
        kubeYAML = nil
        talosSummary = nil
        kubeSummary = nil
        unreadable = []
        summary = nil
        QuickActions.update(summary: nil, labels: labels)
        publishWidgetClusters()
    }

    /// Callers must have authenticated the user first; the lock off drops the enrolled keys too.
    func setLockEnabled(_ enabled: Bool) {
        lock.setEnabled(enabled)
        UserDefaults.standard.set(enabled, forKey: Keys.lock)
        if !enabled { setSecurityKeys(nil) }
    }

    /// Callers must have authenticated the user first. No key left forgets the data key too.
    func setSecurityKeys(_ enrolment: SecurityKeyEnrolment?) {
        let value = enrolment.flatMap { $0.keys.isEmpty ? nil : $0 }
        SecurityKeyStore.current = value
        securityKeys = value
        if value == nil { SecurityKeySession.shared.clear() }
    }

    /// Off: everything kept so far is deleted, with its key.
    func setKeepLastKnown(_ enabled: Bool) {
        keepLastKnown = enabled
        UserDefaults.standard.set(enabled, forKey: Keys.keepLastKnown)
        if !enabled { LastKnownStore.wipe() }
    }

    /// Turns the screenshot mode on or off (or changes its words): drops the data held with
    /// the previous names (widget snapshot, loaded screens) and re-reads the config, whose
    /// context names are masked too.
    func setPrivacyMask(_ enabled: Bool, words: String) async {
        guard enabled != privacyMask || words != privacyWords else { return }
        let affectsData = enabled || privacyMask
        privacyMask = enabled
        privacyWords = words
        UserDefaults.standard.set(enabled, forKey: PrivacyKeys.enabled)
        UserDefaults.standard.set(words, forKey: PrivacyKeys.words)
        TalosClient.setPrivacyMask(enabled: enabled, extraWords: words)
        guard affectsData else { return }
        // Also resets the alert diff, which would otherwise see every node renamed.
        SharedStore.clear()
        // Stored with the old names.
        LastKnownStore.wipe()
        forgetFeatures()
        await reparse()
        dataGeneration += 1
    }

    func unlock() { lock.unlock() }
    func didEnterBackground() { lock.onBackground(now: Self.monotonicNow()) }
    func willEnterForeground() { lock.onForeground(now: Self.monotonicNow()) }

    /// Monotonic seconds that keep counting while the device sleeps (systemUptime does not,
    /// so a locked phone left for hours would not relock the app); immune to clock changes.
    private static func monotonicNow() -> TimeInterval {
        TimeInterval(clock_gettime_nsec_np(CLOCK_MONOTONIC)) / 1_000_000_000
    }

    /// The saved context position; nil for installs from before it was saved.
    nonisolated static var savedContextIndex: Int? {
        UserDefaults.standard.object(forKey: Keys.contextIndex) as? Int
    }

    /// Parses the stored config again, keeping the active context by position (its name
    /// changes when masking is toggled).
    private func reparse() async {
        guard let before = summary else { return }
        let stored = StoredConfigs(talos: yaml, kube: kubeYAML)
        let parsed = await stored.parsed()
        // Both stores parsed before: one that no longer does is kept as it was rather than dropped.
        guard parsed.unreadable.isEmpty else { return }
        let index = before.contexts.firstIndex { $0.name == activeContext }
        applyIndex(talos: yaml, talosSummary: parsed.talos, kube: kubeYAML, kubeSummary: parsed.kube, index: index)
    }

    /// Shows the stored configs `talos` and `kube` (nil: none of that kind) and their parsed
    /// summaries, on the context `preferred` (else the current one). Callers keep at least one.
    private func apply(talos newTalos: String?, talosSummary newTalosSummary: ConfigSummary?,
                       kube newKube: String?, kubeSummary newKubeSummary: ConfigSummary?, preferred: String?) {
        guard let newSummary = ConfigSummary.combined(talos: newTalosSummary, kube: newKubeSummary) else { return }
        forgetFeatures()
        yaml = newTalos
        kubeYAML = newKube
        talosSummary = newTalosSummary
        kubeSummary = newKubeSummary
        summary = newSummary
        activeContext = preferred.flatMap { newSummary.context(named: $0)?.name } ?? newSummary.current
        // Every cluster gets a color of its own; removed ones are forgotten.
        storeColors(assignClusterColors(saved: clusterColors, fingerprints: newSummary.contexts.map(\.fingerprint)))
        storeNames(keepClusterNames(saved: clusterNames, fingerprints: newSummary.contexts.map(\.fingerprint)))
        storeKubeServers(keepClusterNames(saved: kubeServers, fingerprints: newSummary.contexts.map(\.fingerprint)))
        storeSnapshotKeys(keepClusterNames(saved: snapshotKeys, fingerprints: newSummary.contexts.map(\.fingerprint)))
        storeSkippedTalosUpdates(keepClusterNames(saved: skippedTalosUpdates, fingerprints: newSummary.contexts.map(\.fingerprint)))
        storeVpnOnly(keepVpnOnly(saved: vpnOnly, fingerprints: newSummary.contexts.map(\.fingerprint)))
        // Sign-ins and Kubernetes access links go with their clusters.
        let kubeFingerprints = newKubeSummary?.contexts.map(\.fingerprint) ?? []
        storeKubeAccess(keepKubeAccess(kubeAccess, talos: newTalosSummary?.contexts.map(\.fingerprint) ?? [], kube: kubeFingerprints))
        // Omni clusters' sign-ins too: under their identity's auth key (and, from older cores,
        // their Talos context's fingerprint).
        KubeAuthStore.shared.keep(fingerprints: authStoreFingerprints(talos: newTalosSummary?.contexts ?? [],
                                                                      kube: newKubeSummary?.contexts ?? []))
        WakeOnLanStore.shared.keep(fingerprints: newSummary.contexts.map(\.fingerprint))
        LastKnownStore.keep(fingerprints: newSummary.contexts.map(\.fingerprint))
        MetricsStore.keep(fingerprints: newSummary.contexts.map(\.fingerprint))
        AlertmanagerStore.keep(fingerprints: newSummary.contexts.map(\.fingerprint))
        // A removed cluster's background state goes too: its snapshot, unreachable count and snoozes.
        storeMonitorUnwatched(keepVpnOnly(saved: monitorUnwatched, fingerprints: newSummary.contexts.map(\.fingerprint)))
        SharedStore.keep(clusters: newSummary.contexts.map(monitorClusterKey))
        HistoryStore.keep(clusters: newSummary.contexts.map(monitorClusterKey))
        AlertSnoozeStore.keep(clusters: newSummary.contexts.map(\.fingerprint))
        let kept = publicIPReports.filter { report in newSummary.contexts.contains { $0.fingerprint == report.key } }
        if kept.count != publicIPReports.count {
            publicIPReports = kept
            try? PublicIPStore.save(kept)
        }
        QuickActions.update(summary: newSummary, labels: labels)
        publishWidgetClusters()
    }
}
