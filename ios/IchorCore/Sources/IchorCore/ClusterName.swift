import Foundation

/// Longest name one can give a cluster: it titles the overview and labels a quick action.
public let clusterNameMax = 40

/// A name typed for a cluster, trimmed; nil (back to its talosconfig context name) when blank.
public func normalizeClusterName(_ input: String) -> String? {
    let name = String(input.trimmingCharacters(in: .whitespacesAndNewlines).prefix(clusterNameMax))
        .trimmingCharacters(in: .whitespacesAndNewlines)
    return name.nonEmpty
}

/// The names of `saved` (by fingerprint) whose cluster is still among `fingerprints`.
public func keepClusterNames(saved: [String: String], fingerprints: [String]) -> [String: String] {
    let known = Set(fingerprints.filter { !$0.isEmpty })
    return saved.filter { known.contains($0.key) }
}

/// How clusters are called on screen: the name the user gave one (by fingerprint, kept on
/// this device only), else its talosconfig context name. The screenshot mode masks context
/// names, so given names are not shown then: they may be just as revealing.
public struct ClusterLabels: Equatable, Sendable {
    public let names: [String: String]
    public let masked: Bool

    public init(names: [String: String] = [:], masked: Bool = false) {
        self.names = names
        self.masked = masked
    }

    public func of(_ context: ContextSummary) -> String { given(context) ?? context.name }

    /// The name the user gave `context`, if any and if it may be shown.
    public func given(_ context: ContextSummary) -> String? {
        masked || context.fingerprint.isEmpty ? nil : names[context.fingerprint]
    }
}
