import CoreSpotlight
import Foundation
import IchorCore
import UniformTypeIdentifiers

/// Ichor's Spotlight results (SpotlightItems): the clusters on the phone and the Argo CD and
/// Flux apps of the clusters opened, each opening its screen through its share link (the item's
/// identifier, routed like any link once unlocked). Kept with the last known state: nothing is
/// indexed while it is off, and turning it off or wiping the app deletes every item.
@MainActor
enum SpotlightIndex {
    /// The clusters whose apps are indexed, to delete them once the cluster is removed.
    private static let appClustersKey = "spotlightAppClusters"

    /// What AppModel last said: the context on screen, the labels, whether to index.
    private static var active: ContextSummary?
    private static var labels = ClusterLabels()
    private static var enabled = false

    /// Re-indexes the clusters of `summary` (`active` the context on screen) and deletes the
    /// apps of the clusters no longer on it. Not `enabled` (last known state off): deletes all.
    /// A nil summary (not read yet) changes nothing.
    static func update(summary: ConfigSummary?, active context: String, labels: ClusterLabels, enabled: Bool) {
        self.enabled = enabled
        self.labels = labels
        guard enabled else {
            wipe()
            return
        }
        guard let summary else { return }
        active = summary.context(named: context)
        replace(domain: SpotlightItems.clustersDomain, with: SpotlightItems.clusters(summary.contexts, active: context, labels: labels))
        let stale = SpotlightItems.staleAppClusters(indexed: appClusters, current: summary.contexts)
        guard !stale.isEmpty else { return }
        CSSearchableIndex.default().deleteSearchableItems(withDomainIdentifiers: stale.map(SpotlightItems.appsDomain(cluster:)))
        appClusters = appClusters.filter { !stale.contains($0) }
    }

    /// The Argo CD apps ArgoCDStore loaded for `key` (AppModel.argoKey), when that is still the
    /// context on screen.
    static func index(argo apps: [ArgoApp], key: String) {
        indexApps(kind: .argoApp, key: key) { SpotlightItems.argoApps(apps, of: $0, labels: labels) }
    }

    /// The Flux apps FluxStore loaded for `key` (AppModel.fluxKey).
    static func index(flux apps: [FluxApp], key: String) {
        indexApps(kind: .fluxApp, key: key) { SpotlightItems.fluxApps(apps, of: $0, labels: labels) }
    }

    /// Deletes every item: the app wiped, the last known state turned off, the screenshot mode
    /// changed (the names indexed were the previous ones).
    static func wipe() {
        active = nil
        appClusters = []
        CSSearchableIndex.default().deleteSearchableItems(withDomainIdentifiers: [SpotlightItems.clustersDomain, SpotlightItems.appsDomain])
    }

    /// The share link (ichor://open?…) of the Spotlight result `activity` opens, if it is one of ours.
    nonisolated static func link(of activity: NSUserActivity) -> URL? {
        guard activity.activityType == CSSearchableItemActionType,
              let identifier = activity.userInfo?[CSSearchableItemActivityIdentifier] as? String else { return nil }
        return SpotlightItems.link(identifier: identifier).flatMap(URL.init(string:))
    }

    private static func indexApps(kind: SpotlightItem.Kind, key: String, items: (ContextSummary) -> [SpotlightItem]) {
        // Loaded for another context than the one on screen now: its cluster is unknown here.
        guard enabled, let active, !active.clusterID.isEmpty, key.hasPrefix(active.name + "#") else { return }
        if !appClusters.contains(active.clusterID) { appClusters.append(active.clusterID) }
        replace(domain: SpotlightItems.appsDomain(cluster: active.clusterID, kind: kind), with: items(active))
    }

    /// The items of `domain` become `items` (same identifiers are updated, the others go).
    private static func replace(domain: String, with items: [SpotlightItem]) {
        let searchable = items.compactMap(searchableItem)
        let index = CSSearchableIndex.default()
        index.deleteSearchableItems(withDomainIdentifiers: [domain]) { _ in
            guard !searchable.isEmpty else { return }
            index.indexSearchableItems(searchable)
        }
    }

    private static func searchableItem(_ item: SpotlightItem) -> CSSearchableItem? {
        guard let link = TalosClient.shareLink(for: item.target) else { return nil }
        let attributes = CSSearchableItemAttributeSet(contentType: .content)
        attributes.title = item.title
        attributes.contentDescription = switch item.kind {
        case .cluster: String(localized: "Cluster in Ichor")
        case .argoApp: String(localized: "Argo CD app · \(item.clusterLabel)")
        case .fluxApp: String(localized: "Flux app · \(item.clusterLabel)")
        }
        return CSSearchableItem(uniqueIdentifier: link.absoluteString, domainIdentifier: item.domain, attributeSet: attributes)
    }

    private static var appClusters: [String] {
        get { UserDefaults.standard.stringArray(forKey: appClustersKey) ?? [] }
        set { UserDefaults.standard.set(newValue, forKey: appClustersKey) }
    }
}
