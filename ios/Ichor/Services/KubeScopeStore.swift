import Foundation
import IchorCore

/// The namespace the Kubernetes screen lists, picked by the user, by cluster fingerprint (see
/// KubeScope.stored: "" for every namespace). Typed namespaces, when the credentials cannot
/// list them, are kept the same way. Only on this device.
enum KubeScopeStore {
    private static let key = "kubeScopes"

    private static var scopes: [String: String] {
        UserDefaults.standard.dictionary(forKey: key) as? [String: String] ?? [:]
    }

    /// The scope remembered for the cluster `fingerprint`, nil when none was picked.
    static func scope(for fingerprint: String) -> KubeScope? {
        scopes[fingerprint].map { KubeScope.fromStored($0) }
    }

    /// Remembers `scope` for the cluster `fingerprint`; one not chosen forgets it.
    static func set(_ scope: KubeScope, for fingerprint: String) {
        store(settingKubeScope(scope, for: fingerprint, in: scopes))
    }

    /// Forgets the scopes of the clusters no longer imported.
    static func keep(fingerprints: [String]) {
        store(keepClusterNames(saved: scopes, fingerprints: fingerprints))
    }

    private static func store(_ next: [String: String]) {
        guard next != scopes else { return }
        UserDefaults.standard.set(next, forKey: key)
    }
}
