import Foundation
import Ichorgo
import IchorCore

/// An imported context named like a stored cluster (Talos or kubeconfig): the free name it
/// gets, and the stored context of the same cluster it may replace instead.
struct KubeImportConflict: Decodable, Sendable {
    let index: Int
    let suggested: String
    let sameAs: String?
}

/// Clusters added from a kubeconfig, without Talos (see TalosClient for the conventions). Their
/// calls take the stored kubeconfig as `config`; the Talos ones then fail with a clear message.
extension TalosClient {
    /// The config a pasted, scanned or opened text holds: the text itself, or the YAML of a
    /// compressed "ichor-config:" payload (too large a config for a QR code otherwise).
    static func decodeImportText(_ text: String) async throws -> String {
        try await run { IchorgoDecodeImportText(text, $0) }
    }

    /// Whether `yaml` is a kubeconfig (the import then takes the kubeconfig flow).
    static func isKubeconfig(_ yaml: String) async -> Bool {
        await Task.detached(priority: .userInitiated) { IchorgoIsKubeconfig(yaml) }.value
    }

    /// One row per context of `yaml` (an imported kubeconfig or the stored one), sorted by
    /// name, each importable or carrying why it is not.
    static func parseKubeconfig(_ yaml: String) async throws -> ConfigSummary {
        try await json { IchorgoParseKubeconfig(yaml, $0) }
    }

    /// The contexts of `added` named like a stored cluster of either store.
    static func kubeImportConflicts(stored: String, talos: String, added: String) async throws -> [KubeImportConflict] {
        try await json { IchorgoKubeImportConflicts(stored, talos, added, $0) }
    }

    /// `stored` (the stored kubeconfig, "" for none) with the importable contexts of `added`
    /// added, except those `choices` skips (see kubeImportChoices). Throws when none is added.
    static func mergeKubeconfig(stored: String, talos: String, added: String, choices: [KubeImportChoice]) async throws -> String {
        let encoded = try TalosJSON.encode(choices)
        return try await run { IchorgoMergeKubeconfig(stored, talos, added, encoded, $0) }
    }

    /// `stored` without `context`; "" once no context is left (the store is then deleted).
    static func removeKubeContext(stored: String, context: String) async throws -> String {
        try await run { IchorgoRemoveKubeContext(stored, context, $0) }
    }

    /// `context` alone as a kubeconfig, for the user to save: a credential. With `kubeServer`
    /// (the API address set for the cluster, "" for the kubeconfig's), the export points there.
    static func exportKubeContext(stored: String, context: String, kubeServer: String = "") async throws -> String {
        try await run { IchorgoExportKubeContextFor(stored, context, kubeServer, $0) }
    }

    /// The contexts of the talosconfig `added` named like a stored cluster, of the talosconfig
    /// `stored` ("" for none) or the kubeconfig `kube` ("" for none): the free name each gets,
    /// and the stored context of the same cluster (same CA) it may replace instead.
    static func talosImportConflicts(stored: String, kube: String, added: String) async throws -> [ImportConflict] {
        try await json { IchorgoTalosImportConflicts(stored, kube, added, $0) }
    }

    /// `stored` ("" for none) with the contexts of `added` added: a context per cluster, names
    /// unique across both stores. A stored context is never overwritten: one of that name is
    /// added as name-1, unless `choices` (a JSON array of {index, name, replace}, see
    /// MergeConfig in Go) names it or replaces the same cluster.
    static func mergeTalosconfig(stored: String, kube: String, added: String, choices: String = "") async throws -> String {
        try await run { IchorgoMergeTalosconfig(stored, kube, added, choices, $0) }
    }

    /// The cluster's nodes as the Kubernetes API sees them, and its version: the home of a
    /// cluster added from a kubeconfig.
    func kubeNodes() async throws -> KubeNodesOverview {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in IchorgoKubeNodes(config, context, kubeServer, $0) }
    }
}
