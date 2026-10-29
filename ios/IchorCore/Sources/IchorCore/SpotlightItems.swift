import Foundation

/// A Spotlight result: a cluster the phone holds, or an Argo CD or Flux app of one. It opens
/// its screen through a share link (its identifier), so it carries names only: never an
/// endpoint, a node address or a credential.
public struct SpotlightItem: Equatable, Sendable {
    public enum Kind: String, Equatable, Sendable { case cluster, argoApp = "argo", fluxApp = "flux" }

    public let kind: Kind
    public let title: String
    /// How the app names the item's cluster (the title itself for a cluster).
    public let clusterLabel: String
    /// The group Spotlight deletes it with (see SpotlightItems).
    public let domain: String
    /// The screen it opens, its cluster set.
    public let target: ShareTarget
}

/// What Ichor indexes in Spotlight: every cluster (domain `clusters`) and the GitOps apps of the
/// clusters opened (`apps.<cluster>.argo`, `apps.<cluster>.flux`). Spotlight deletes a domain's
/// subdomains with it, so `apps` drops every app and `apps.<cluster>` one cluster's.
public enum SpotlightItems {
    public static let clustersDomain = "clusters"
    public static let appsDomain = "apps"

    /// The domain of `cluster`'s apps (a `clusterID`: letters only, no dot).
    public static func appsDomain(cluster: String) -> String { "\(appsDomain).\(cluster)" }

    /// The domain of `cluster`'s Argo CD or Flux apps: each store's load replaces its own.
    public static func appsDomain(cluster: String, kind: SpotlightItem.Kind) -> String {
        "\(appsDomain(cluster: cluster)).\(kind.rawValue)"
    }

    /// One item per cluster of `contexts` (the `active` context standing for its own), named as
    /// on screen; none for a context without a `clusterID`, which no share link can name.
    public static func clusters(_ contexts: [ContextSummary], active: String?, labels: ClusterLabels) -> [SpotlightItem]
    {
        clusterContexts(contexts, active: active).filter { !$0.clusterID.isEmpty }.map { context in
            let label = labels.of(context)
            return SpotlightItem(
                kind: .cluster, title: label, clusterLabel: label, domain: clustersDomain,
                target: ShareTarget(cluster: context.clusterID, target: .cluster))
        }
    }

    /// The Argo CD apps of `context`'s cluster; none without a `clusterID`.
    public static func argoApps(_ apps: [ArgoApp], of context: ContextSummary, labels: ClusterLabels) -> [SpotlightItem]
    {
        appItems(
            apps.map { ShareTarget.argoApp(namespace: $0.namespace, name: $0.name) }, kind: .argoApp, of: context,
            labels: labels)
    }

    /// The Flux apps of `context`'s cluster; none without a `clusterID`.
    public static func fluxApps(_ apps: [FluxApp], of context: ContextSummary, labels: ClusterLabels) -> [SpotlightItem]
    {
        appItems(
            apps.map { ShareTarget.fluxApp(kind: $0.kind, namespace: $0.namespace, name: $0.name) }, kind: .fluxApp,
            of: context, labels: labels)
    }

    /// Of the clusters whose apps were indexed (`indexed`), those no longer on the phone: their
    /// app domains are to be deleted.
    public static func staleAppClusters(indexed: [String], current: [ContextSummary]) -> [String] {
        let kept = Set(current.map(\.clusterID))
        return indexed.filter { !kept.contains($0) }
    }

    /// The in-app link (ichor://open?…) a Spotlight result opens: its identifier is the web
    /// share link. Nil for anything else.
    public static func link(identifier: String) -> String? {
        guard identifier.hasPrefix("https://") else { return nil }
        return appShareLink(web: identifier)
    }

    private static func appItems(
        _ targets: [ShareTarget], kind: SpotlightItem.Kind, of context: ContextSummary,
        labels: ClusterLabels
    ) -> [SpotlightItem] {
        guard !context.clusterID.isEmpty else { return [] }
        let label = labels.of(context)
        let domain = appsDomain(cluster: context.clusterID, kind: kind)
        var seen = Set<ShareTarget>()
        return targets.filter { !$0.name.isEmpty && seen.insert($0).inserted }.map { target in
            var target = target
            target.cluster = context.clusterID
            return SpotlightItem(kind: kind, title: target.name, clusterLabel: label, domain: domain, target: target)
        }
    }
}
