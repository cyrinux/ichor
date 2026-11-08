import Foundation
import IchorCore

/// The two stored configs: the talosconfig (Talos clusters) and the kubeconfig (clusters added
/// from a kubeconfig, without Talos). Either may be absent, not both once a cluster is stored.
struct StoredConfigs: Sendable {
    var talos: String?
    var kube: String?
    /// A sealed item is stored for each, even when it could not be decrypted (`talos`/`kube` nil).
    var talosStored: Bool
    var kubeStored: Bool

    init(talos: String?, kube: String?) {
        self.talos = talos
        self.kube = kube
        talosStored = talos != nil
        kubeStored = kube != nil
    }

    /// Both as stored (SecureConfigStore); nothing can be read before the first unlock.
    static func load() -> StoredConfigs {
        var stored = StoredConfigs(
            talos: SecureConfigStore.loadText(.talosconfig), kube: SecureConfigStore.loadText(.kubeconfig))
        stored.talosStored = stored.talos != nil || SecureConfigStore.isStored(.talosconfig)
        stored.kubeStored = stored.kube != nil || SecureConfigStore.isStored(.kubeconfig)
        return stored
    }

    /// The config the Go calls for `context` take: the kubeconfig for a cluster added from one.
    func config(for context: ContextSummary?) -> String? {
        guard let context else { return talos ?? kube }
        return context.isKube ? kube : talos
    }

    /// The parsed stores, and those stored but not readable (not decrypted, or not parsed):
    /// while there is one, the app must neither show the other alone nor write (see unreadableConfigs).
    /// A store whose text is unchanged since its summary was cached (ConfigSummaryCache, same
    /// screenshot-mode masking) is not parsed again. `generation`: the masking they were made
    /// under (TalosClient.privacyGeneration, read before parsing), for ConfigSummaryCache.store.
    func parsed() async -> (
        talos: ConfigSummary?, kube: ConfigSummary?, unreadable: [StoredConfigKind], generation: Int
    ) {
        let cache = ConfigSummaryCache.shared
        let record = cache.load()
        let generation = TalosClient.privacyGeneration()
        var talosSummary: ConfigSummary?
        var kubeSummary: ConfigSummary?
        var cached: [Bool] = []
        if let talos, let found = await cache.summary(.talosconfig, of: talos, record: record, generation: generation) {
            talosSummary = found.summary
            cached.append(found.cached)
        }
        if let kube, let found = await cache.summary(.kubeconfig, of: kube, record: record, generation: generation) {
            kubeSummary = found.summary
            cached.append(found.cached)
        }
        let contexts = (talosSummary?.contexts.count ?? 0) + (kubeSummary?.contexts.count ?? 0)
        let source = !cached.isEmpty && cached.allSatisfy { $0 } ? "cached" : "parsed"
        StartupTrace.markOnce("config parsed", detail: "\(contexts) contexts, summary: \(source)")
        let unreadable = unreadableConfigs([
            .talosconfig: StoredConfigStatus(stored: talosStored, parsed: talosSummary != nil),
            .kubeconfig: StoredConfigStatus(stored: kubeStored, parsed: kubeSummary != nil),
        ])
        return (talosSummary, kubeSummary, unreadable, generation)
    }
}
