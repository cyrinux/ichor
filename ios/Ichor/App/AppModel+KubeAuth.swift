import Foundation
import IchorCore

/// A stored context that signs in through a method: what the sign-in screens act on. A
/// kubeconfig context, or a talosconfig context managed by Sidero Omni (`talos`).
struct KubeSignInTarget: Identifiable, Equatable {
    /// The stored config the context is in: the kubeconfig, or the talosconfig when `talos`.
    let kube: String
    let context: String
    /// The cluster's fingerprint (the auth state's key).
    let fingerprint: String
    /// How the cluster is called on screen.
    let label: String
    var talos = false

    var id: String { fingerprint + "|" + context }
}

/// Sign-ins of the clusters added from a kubeconfig, and the Kubernetes access of Talos
/// clusters (K5).
extension AppModel {
    /// The stored clusters added from a kubeconfig.
    var kubeContexts: [ContextSummary] { summary?.contexts.filter(\.isKube) ?? [] }

    /// The stored kubeconfig context the Kubernetes calls of `context` (a Talos cluster) go
    /// through, nil when they use the admin kubeconfig Talos issues.
    func kubeLink(for context: ContextSummary?) -> KubeLink? {
        guard let kubeYAML, let name = kubeAccessContext(of: context, links: kubeAccess, kubeContexts: kubeContexts) else { return nil }
        return KubeLink(config: kubeYAML, context: name)
    }

    /// The kubeconfig cluster `talos` uses for Kubernetes, nil for the Talos admin kubeconfig.
    func kubeAccessTarget(of talos: ContextSummary) -> ContextSummary? {
        guard let name = kubeAccessContext(of: talos, links: kubeAccess, kubeContexts: kubeContexts) else { return nil }
        return summary?.context(named: name)
    }

    /// Points the Kubernetes calls of the Talos cluster `talos` at the kubeconfig cluster
    /// `kube` (nil: back to the Talos admin kubeconfig); its screens reload.
    func setKubeAccess(_ kube: ContextSummary?, for talos: ContextSummary) {
        guard !talos.isKube, !talos.fingerprint.isEmpty else { return }
        var links = kubeAccess
        links[talos.fingerprint] = kube.flatMap { $0.isKube && !$0.fingerprint.isEmpty ? $0.fingerprint : nil }
        storeKubeAccess(links)
        if talos.fingerprint == activeSummary?.fingerprint { reloadKubernetes() }
    }

    /// The sign-in `context` needs, nil when it has static credentials: its own for a cluster
    /// added from a kubeconfig, that of its Kubernetes access for a linked Talos cluster.
    func signInTarget(for context: ContextSummary?) -> KubeSignInTarget? {
        guard let context else { return nil }
        if context.omni {
            guard let yaml, context.signIn != nil else { return nil }
            return KubeSignInTarget(kube: yaml, context: context.name, fingerprint: context.fingerprint, label: labels.of(context), talos: true)
        }
        guard let kubeYAML else { return nil }
        let kube = context.isKube ? context : kubeAccessTarget(of: context)
        guard let kube, kube.signIn != nil else { return nil }
        return KubeSignInTarget(kube: kubeYAML, context: kube.name, fingerprint: kube.fingerprint, label: labels.of(kube))
    }

    /// The sign-in of the cluster on screen, if it has one.
    var activeSignInTarget: KubeSignInTarget? { signInTarget(for: activeSummary) }
}
