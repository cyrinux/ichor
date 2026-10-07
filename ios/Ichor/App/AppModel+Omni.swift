import Foundation
import IchorCore

/// A stored Talos context reached through Omni: what its sign-in screens act on.
struct OmniSignInTarget: Identifiable, Equatable {
    /// The stored talosconfig.
    let config: String
    let context: String
    /// The cluster's fingerprint (the auth state's key).
    let fingerprint: String
    /// How the cluster is called on screen.
    let label: String
    /// The account's email; a browser sign-in needs one.
    let identity: String?

    var id: String { fingerprint + "|" + context }

    var canSignInWithBrowser: Bool { !(identity ?? "").isEmpty }
}

/// Sign-ins of the Talos clusters reached through Omni.
extension AppModel {
    /// The Omni sign-in of `context`, nil when it is not reached through Omni.
    func omniTarget(for context: ContextSummary?) -> OmniSignInTarget? {
        guard let context, context.isOmni, let yaml else { return nil }
        return OmniSignInTarget(config: yaml, context: context.name, fingerprint: context.fingerprint,
                                label: labels.of(context), identity: context.identity)
    }

    /// The Omni sign-in of the cluster on screen, if it is reached through Omni.
    var activeOmniTarget: OmniSignInTarget? { omniTarget(for: activeSummary) }

    /// The clusters whose sign-ins the auth store holds: kubeconfig and Omni clusters.
    var signInFingerprints: [String] {
        let contexts = summary?.contexts ?? []
        return authStoreFingerprints(talos: contexts.filter { !$0.isKube }, kube: contexts.filter(\.isKube))
    }
}
