import Foundation
import IchorCore

/// Live CPU and memory for the overview's summary: the Go core's ClusterStats, sampled while
/// the overview is visible, the app active and the setting on (see OverviewView). The samples
/// (IchorCore ClusterLive) survive leaving the overview and coming back, but not another key
/// (the cluster, or the screenshot mode generation). Same as Android's ClusterLiveViewModel.
@Observable
@MainActor
final class ClusterLiveStore {
    static let shared = ClusterLiveStore()

    private(set) var live = ClusterLive()
    private var key = ""

    /// What was sampled for key, nil for another cluster.
    func live(for key: String) -> ClusterLive? { key == self.key ? live : nil }

    /// Samples until cancelled; each sample asks every node, so a cluster of many `nodes` is
    /// sampled less often (clusterPollSeconds).
    func poll(with client: TalosClient, key: String, nodes: Int) async {
        if key != self.key {
            self.key = key
            live = ClusterLive()
        } else {
            live.resume()
        }
        while !Task.isCancelled {
            do {
                let sample = try await client.clusterStats()
                // Leaving the screen (or another cluster) is not a sample.
                guard !Task.isCancelled, key == self.key else { return }
                live.record(sample)
            } catch {
                guard !Task.isCancelled, key == self.key else { return }
                live.recordFailure()
            }
            try? await Task.sleep(for: .seconds(live.nextDelay(nodeCount: nodes)))
        }
    }

    /// Forgets the samples, e.g. when live stats are turned off, so a later start is not stale.
    func clear() {
        live = ClusterLive()
    }
}

/// The "Live cluster usage" setting: on by default, off to spare mobile data and battery.
enum LiveStatsSettings {
    static let key = "liveClusterStats"

    static var enabled: Bool {
        get { UserDefaults.standard.object(forKey: key) as? Bool ?? true }
        set { UserDefaults.standard.set(newValue, forKey: key) }
    }
}
