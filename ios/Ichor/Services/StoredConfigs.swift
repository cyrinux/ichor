import Foundation
import IchorCore

/// The two stored configs: the talosconfig (Talos clusters) and the kubeconfig (clusters added
/// from a kubeconfig, without Talos). Either may be absent, not both once a cluster is stored.
struct StoredConfigs: Sendable {
    var talos: String?
    var kube: String?

    /// Both as stored (SecureConfigStore); nothing can be read before the first unlock.
    static func load() -> StoredConfigs {
        StoredConfigs(talos: SecureConfigStore.loadText(.talosconfig), kube: SecureConfigStore.loadText(.kubeconfig))
    }

    /// The config the Go calls for `context` take: the kubeconfig for a cluster added from one.
    func config(for context: ContextSummary?) -> String? {
        guard let context else { return talos ?? kube }
        return context.isKube ? kube : talos
    }

    /// The parsed stores; one that does not parse is left out (as if absent), like a single
    /// unreadable talosconfig always was.
    func parsed() async -> (talos: ConfigSummary?, kube: ConfigSummary?) {
        var talosSummary: ConfigSummary?
        var kubeSummary: ConfigSummary?
        if let talos { talosSummary = try? await TalosClient.parse(talos) }
        if let kube { kubeSummary = try? await TalosClient.parseKubeconfig(kube) }
        return (talosSummary, kubeSummary)
    }
}
