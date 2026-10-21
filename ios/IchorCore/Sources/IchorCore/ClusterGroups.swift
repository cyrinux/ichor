import Foundation

// The cluster list, once long, in sections: per provider, and per account or project within
// one. Same rules on Android (model/ClusterGroups.kt).

/// From this many clusters the list is grouped: the page dots turn into a counter at the same size.
public let clusterGroupFrom = 6

/// Where a cluster runs.
public enum ClusterProvider: String, CaseIterable, Sendable {
    case talos, omni, aws, gcp, azure, digitalOcean, rancher, kubernetes

    /// Its bundled logo (appicons).
    public var logo: String {
        switch self {
        case .talos, .omni: "talos"
        case .aws: "aws"
        case .gcp: "google-cloud"
        case .azure: "azure"
        case .digitalOcean: "digital-ocean"
        case .rancher: "rancher"
        case .kubernetes: "kubernetes"
        }
    }

    /// Its name (not translated).
    public var brand: String {
        switch self {
        case .talos: "Talos"
        case .omni: "Omni"
        case .aws: "AWS"
        case .gcp: "Google Cloud"
        case .azure: "Azure"
        case .digitalOcean: "DigitalOcean"
        case .rancher: "Rancher"
        case .kubernetes: "Kubernetes"
        }
    }
}

/// A section of the cluster list: its `provider` and, when the contexts tell, the `owner` (a
/// shortened AWS account ID, a GCP project, an Omni identity). `key` tells groups apart; it is
/// not a title.
public struct ClusterGroup: Equatable, Identifiable, Sendable {
    public let key: String
    public let provider: ClusterProvider
    public let owner: String?
    public let contexts: [ContextSummary]

    public var id: String { key }
    public var logo: String { provider.logo }

    public init(key: String, provider: ClusterProvider, owner: String? = nil, contexts: [ContextSummary] = []) {
        self.key = key
        self.provider = provider
        self.owner = owner
        self.contexts = contexts
    }
}

private let logoProviders = Dictionary(
    ClusterProvider.allCases.filter { $0 != .omni }.map { ($0.logo, $0) },
    uniquingKeysWith: { first, _ in first }
)

/// The group `context` belongs to, without its contexts. Screenshot mode (`masked`) leaves the
/// owner out: the clusters of every account of a provider then share one group.
public func clusterGroupKey(_ context: ContextSummary, masked: Bool) -> ClusterGroup {
    let cloud = context.isKube ? parseCloudContext(context.name) : nil
    let provider: ClusterProvider
    if context.omni {
        provider = .omni
    } else if !context.isKube {
        provider = .talos
    } else if let cloud {
        provider = cloud.provider == CloudContext.eks ? .aws : .gcp
    } else {
        provider = logoProviders[clusterLogo(context)] ?? .kubernetes
    }
    // Whose clusters they are: the id keys the group, the owner titles it.
    var id: String?
    var owner: String?
    if masked {
        // Neither shown nor told apart.
    } else if context.omni {
        let identity = context.identity ?? ""
        let authKey = context.authKey ?? ""
        id = authKey.isEmpty ? (identity.isEmpty ? nil : identity) : authKey
        owner = identity.isEmpty ? nil : identity
    } else if let cloud, !cloud.owner.isEmpty {
        id = cloud.owner
        owner = cloud.shortOwner
    }
    let key = [provider.rawValue.lowercased(), id].compactMap { $0 }.joined(separator: ":")
    return ClusterGroup(key: key, provider: provider, owner: owner)
}

/// `contexts` in groups, in the order of each group's first cluster, the clusters in their
/// stored order; nil (a flat list) under `clusterGroupFrom` clusters.
public func clusterGroups(_ contexts: [ContextSummary], masked: Bool) -> [ClusterGroup]? {
    guard contexts.count >= clusterGroupFrom else { return nil }
    let keyed = contexts.map { ($0, clusterGroupKey($0, masked: masked)) }
    let members = Dictionary(grouping: keyed, by: { $0.1.key })
    var seen = Set<String>()
    return keyed.compactMap { _, group in
        guard seen.insert(group.key).inserted else { return nil }
        return ClusterGroup(key: group.key, provider: group.provider, owner: group.owner,
                            contexts: members[group.key]?.map(\.0) ?? [])
    }
}
