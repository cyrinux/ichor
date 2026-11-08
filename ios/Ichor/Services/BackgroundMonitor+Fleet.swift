import Foundation
import IchorCore

extension BackgroundMonitor {
    /// Ranks every cluster's last check (the fleet screen's rules, through the Go core; the
    /// demo's fleet when the demo is the only cluster) and keeps the compact glance in the App
    /// Group for the fleet widget and the watch, which cannot call the Go core. Names as the app
    /// shows them (masked in screenshot mode). A ranking error keeps the last glance: the widget
    /// never goes blank because of it. The widgets are reloaded by the SharedStore.save after it.
    static func publishFleet(
        contexts: [ContextSummary], snapshots: [String: ClusterSnapshot], reachability: [String: Reachability],
        now: Date
    ) async {
        let names = UserDefaults.standard.dictionary(forKey: "clusterNames") as? [String: String] ?? [:]
        let masked = UserDefaults.standard.bool(forKey: PrivacyKeys.enabled)
        let summary: FleetSummary
        do {
            if demoFleetOnly(contexts) {
                summary = try await TalosClient.demoFleetSummary(now: now)
            } else {
                let entries = fleetEntries(
                    snapshots: snapshots, reachability: reachability, contexts: contexts,
                    labels: ClusterLabels(names: names, masked: masked))
                summary = try await TalosClient.fleetSummary(entries: entries, now: now)
            }
        } catch {
            return
        }
        SharedStore.saveFleet(FleetGlance(summary: summary) { $0.text(masked: masked) })
    }
}
