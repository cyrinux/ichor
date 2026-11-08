import Foundation
import Observation
import IchorCore

// Per-cluster settings (VPN only, background watch, recorded actions) and what follows them
// (widget, Spotlight). The values themselves are SettingsStore's.
extension AppModel {
    /// Whether the cluster on screen is set to be reached over a VPN only and none is up.
    var vpnHeldBack: Bool {
        heldBackForVpn(vpnOnly: settings.vpnOnly, fingerprint: activeSummary?.fingerprint, vpnUp: VpnMonitor.shared.up)
    }

    /// Sets whether `context` is reached over a VPN only; the cluster on screen reloads (with
    /// the VPN off, the screens then say to connect it).
    func setVpnOnly(_ on: Bool, for context: ContextSummary) {
        guard !context.fingerprint.isEmpty else { return }
        let fingerprint = context.fingerprint
        settings.storeVpnOnly(on ? settings.vpnOnly.union([fingerprint]) : settings.vpnOnly.subtracting([fingerprint]))
        if context.fingerprint == activeSummary?.fingerprint { dataGeneration += 1 }
    }

    /// Whether the background checks watch `context`'s cluster (off on any of its contexts: not).
    func watchesInBackground(_ context: ContextSummary) -> Bool {
        watchedInBackground(context, contexts: summary?.contexts ?? [], unwatched: settings.monitorUnwatched)
    }

    /// Turns the background checks of `context`'s cluster on or off.
    func setWatchInBackground(_ on: Bool, for context: ContextSummary) {
        settings.storeMonitorUnwatched(
            settingWatched(on, for: context, contexts: summary?.contexts ?? [], unwatched: settings.monitorUnwatched))
    }

    /// Whether `context`'s cluster records actions (a Kubernetes Event per change).
    func recordsActions(_ context: ContextSummary) -> Bool {
        clusterFlagged(context, contexts: summary?.contexts ?? [], members: settings.auditEventClusters)
    }

    /// Turns "Record actions in the cluster" on or off for `context`'s cluster (all its contexts).
    func setRecordActions(_ on: Bool, for context: ContextSummary) {
        settings.storeAuditEventClusters(
            settingClusterFlag(
                on, for: context, contexts: summary?.contexts ?? [],
                members: settings.auditEventClusters))
    }

    /// Tells the widget which clusters it can show, named as on screen, and which one is active
    /// (what a widget with no cluster chosen shows).
    // Internal, not private: AppModel+Clusters.swift uses it.
    func publishWidgetClusters() {
        // A widget set to a cluster opens that cluster's screen (a share link, routed after unlock).
        let clusters =
            summary.map { summary in
                widgetClusters(summary.contexts, active: activeContext, labels: labels) { context in
                    guard !context.clusterID.isEmpty else { return nil }
                    var target = ShareTarget.screen(.cluster)
                    target.cluster = context.clusterID
                    return TalosClient.shareLink(for: target).flatMap { appShareLink(web: $0.absoluteString) }
                }
            } ?? []
        SharedStore.publish(clusters: clusters, active: activeSummary.map(monitorClusterKey))
    }

    /// Spotlight lists its items outside the app: only with the last known state kept, no app
    /// lock and no screenshot mode (any of them on deletes what was indexed).
    // Internal, not private: AppModel+Clusters.swift uses it.
    func updateSpotlight() {
        SpotlightIndex.update(
            summary: summary, active: activeContext, labels: labels,
            enabled: settings.keepLastKnown && !lockStore.lock.enabled && !settings.privacyMask)
    }

    /// The VPN came up: a VPN-only cluster on screen reloads.
    // Internal, not private: AppModel.swift uses it.
    func reloadIfHeldBack() {
        if let shown = activeSummary?.fingerprint, settings.vpnOnly.contains(shown) { dataGeneration += 1 }
    }
}
