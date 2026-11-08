import AppIntents
import BackgroundTasks
import Foundation
import IchorCore
import UIKit
import UserNotifications
import WidgetKit

/// Shared with the widget extension (App Group): a snapshot per cluster (by monitor key, see
/// monitorClusterKey), the clusters the widget can pick and the active one. Each change is also
/// sent to the Apple Watch (WatchLink).
enum SharedStore {
    static var defaults: UserDefaults { UserDefaults(suiteName: SharedSnapshot.suite) ?? .standard }

    /// Every cluster's last snapshot; the single one of older versions goes to `activeCluster`.
    static func snapshots(activeCluster: String?) -> [String: ClusterSnapshot] {
        migratedSnapshots(
            SharedSnapshot.loadAll(from: defaults), legacy: SharedSnapshot.load(from: defaults),
            activeCluster: activeCluster)
    }

    /// Stores `snapshots` (the older versions' single one goes) and reloads the widget.
    static func save(_ snapshots: [String: ClusterSnapshot]) {
        if !snapshots.isEmpty, let data = try? JSONEncoder().encode(snapshots) {
            defaults.set(data, forKey: SharedSnapshot.snapshotsKey)
        } else {
            defaults.removeObject(forKey: SharedSnapshot.snapshotsKey)
        }
        defaults.removeObject(forKey: SharedSnapshot.key)
        WidgetCenter.shared.reloadAllTimelines()
        // After each run (and when cleared): the Apple Watch shows the same.
        WatchLink.shared.send()
    }

    /// Only the state of the clusters still stored (monitor keys): a removed cluster's snapshot
    /// and unreachable count go with it.
    static func keep(clusters: [String]) {
        let stored = SharedSnapshot.loadAll(from: defaults)
        let kept = keepingClusters(stored, clusters: clusters)
        if kept.count != stored.count { save(kept) }
        let reach = BackgroundMonitor.reachability()
        let keptReach = keepingClusters(reach, clusters: clusters)
        if keptReach.count != reach.count { BackgroundMonitor.saveReachability(keptReach) }
    }

    /// Forgets every snapshot (a config replaced, the screenshot mode toggled): the next run is a
    /// baseline. The fleet glance goes too: its names may be the other mode's.
    static func clear() {
        saveFleet(nil)
        save([:])
        BackgroundMonitor.saveReachability([:])
    }

    /// Keeps the fleet glance for the fleet widget and the watch (nil: forgets it). The widgets
    /// reload with the next save, which follows each run.
    static func saveFleet(_ glance: FleetGlance?) {
        if let glance, !glance.rows.isEmpty, let data = try? JSONEncoder().encode(glance) {
            defaults.set(data, forKey: SharedFleet.key)
        } else {
            defaults.removeObject(forKey: SharedFleet.key)
        }
    }

    /// The clusters the widget can pick, named as on screen, and the active one (its default).
    static func publish(clusters: [WidgetCluster], active: String?) {
        let data = clusters.isEmpty ? nil : (try? JSONEncoder().encode(clusters))
        guard
            data != defaults.data(forKey: SharedSnapshot.clustersKey)
                || active != SharedSnapshot.activeCluster(from: defaults)
        else { return }
        if let data {
            defaults.set(data, forKey: SharedSnapshot.clustersKey)
        } else {
            defaults.removeObject(forKey: SharedSnapshot.clustersKey)
        }
        if let active {
            defaults.set(active, forKey: SharedSnapshot.activeKey)
        } else {
            defaults.removeObject(forKey: SharedSnapshot.activeKey)
        }
        // A cluster renamed since the last check: the fleet widget names it as on screen too.
        if let fleet = SharedFleet.load(from: defaults) { saveFleet(fleet.renamed(clusters)) }
        WidgetCenter.shared.reloadAllTimelines()
        // The active cluster changed, or a cluster was added, renamed or masked.
        WatchLink.shared.send()
        // The Siri phrases that name a cluster ("What is the state of <cluster>…").
        IchorShortcuts.updateAppShortcutParameters()
    }
}

/// Best-effort background checks: iOS decides when refresh tasks run (often every 15-60
/// minutes). The config can only be decrypted while the device is unlocked, so checks made
/// while it is locked are skipped (and with a security key required, once iOS closed the app).
/// Each run checks every cluster not turned off. Same alert rules as Android (IchorCore.evaluate).
