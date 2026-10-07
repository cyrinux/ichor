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

/// A managed cluster as its cloud's CLI names its context: `provider` (`eks`, `gke`), the
/// region or zone, the `owner` (AWS account ID, GCP project) and the cluster's own name.
public struct CloudContext: Equatable, Sendable {
    public static let eks = "EKS"
    public static let gke = "GKE"

    public let provider: String
    public let location: String
    public let owner: String
    public let cluster: String

    /// The owner for a list row: an AWS account ID shortened (123456789012 becomes 12…12), a GCP project whole.
    public var shortOwner: String {
        provider == Self.eks && owner.count > 4 ? "\(owner.prefix(2))…\(owner.suffix(2))" : owner
    }
}

/// `name` as an EKS or GKE context name, nil when it is neither.
public func parseCloudContext(_ name: String) -> CloudContext? {
    // aws eks update-kubeconfig: the cluster ARN.
    if case let (region, account, cluster)? = match(#"^arn:aws(?:-[a-z]+)*:eks:([a-z0-9-]+):([0-9]+):cluster/(.+)$"#, name) {
        return CloudContext(provider: CloudContext.eks, location: region, owner: account, cluster: cluster)
    }
    // gcloud container clusters get-credentials: gke_<project>_<location>_<cluster>. Neither
    // part has an underscore (a domain-scoped project is example.com:name).
    if case let (project, location, cluster)? = match(#"^gke_([a-z0-9.:-]+)_([a-z0-9-]+)_([a-z0-9-]+)$"#, name) {
        return CloudContext(provider: CloudContext.gke, location: location, owner: project, cluster: cluster)
    }
    return nil
}

/// The three groups of `pattern` in the whole of `name`, nil when it does not match.
private func match(_ pattern: String, _ name: String) -> (String, String, String)? {
    guard let regex = try? NSRegularExpression(pattern: pattern),
          let found = regex.firstMatch(in: name, range: NSRange(name.startIndex..., in: name)),
          let first = Range(found.range(at: 1), in: name),
          let second = Range(found.range(at: 2), in: name),
          let third = Range(found.range(at: 3), in: name)
    else { return nil }
    return (String(name[first]), String(name[second]), String(name[third]))
}

/// How clusters are called on screen: the name the user gave one (by fingerprint, kept on
/// this device only), else its cluster name when its context name is an EKS or GKE one,
/// else its context name. The screenshot mode masks context names, so given names are not shown
/// then: they may be just as revealing.
public struct ClusterLabels: Equatable, Sendable {
    public let names: [String: String]
    public let masked: Bool

    public init(names: [String: String] = [:], masked: Bool = false) {
        self.names = names
        self.masked = masked
    }

    public func of(_ context: ContextSummary) -> String { given(context) ?? cloud(context)?.cluster ?? context.name }

    /// `context`'s name as an EKS or GKE one, for the location and owner under its label; nil when masked.
    public func cloud(_ context: ContextSummary) -> CloudContext? { masked ? nil : parseCloudContext(context.name) }

    /// The name the user gave `context`, if any and if it may be shown.
    public func given(_ context: ContextSummary) -> String? {
        masked || context.fingerprint.isEmpty ? nil : names[context.fingerprint]
    }
}
