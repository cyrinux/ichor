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

    /// The node rows on screen (the Nodes screen's), by address: part of the focus set.
    @ObservationIgnored private(set) var visibleNodes: Set<String> = []

    func showing(_ node: String) { visibleNodes = visibleNodes.union([node]) }

    func hiding(_ node: String) { visibleNodes = visibleNodes.subtracting([node]) }

    /// Samples until cancelled. Up to the dense threshold each sample asks every node; above it,
    /// most ask only the focus set (`focus`, the problem nodes, and the rows on screen) and every
    /// clusterFullSampleEvery-th every node, the usage merged from each node's last counters
    /// (IchorCore ClusterLive). A cluster of many `nodes` is also sampled less often.
    func poll(with client: TalosClient, key: String, nodes: Int, focus: Set<String>) async {
        if key != self.key {
            self.key = key
            live = ClusterLive()
        } else {
            live.resume()
        }
        // forget() (a screenshot mode switch) ends it too.
        while !Task.isCancelled, key == self.key {
            if let asked = live.nodesToSample(nodeCount: nodes, focus: focus.union(visibleNodes)) {
                await sample(with: client, key: key, nodes: asked)
            } else {
                live.skipTick()
            }
            guard !Task.isCancelled, key == self.key else { return }
            try? await Task.sleep(for: .seconds(live.nextDelay(nodeCount: nodes)))
        }
    }

    /// One sample of `nodes` ([]: every node), recorded unless the poll ended meanwhile.
    private func sample(with client: TalosClient, key: String, nodes: [String]) async {
        do {
            let taken = try await client.clusterStats(nodes: nodes)
            // Leaving the screen (or another cluster) is not a sample.
            guard !Task.isCancelled, key == self.key else { return }
            live.record(taken)
        } catch {
            guard !Task.isCancelled, key == self.key else { return }
            live.recordFailure()
        }
    }

    /// Forgets the samples, e.g. when live stats are turned off, so a later start is not stale.
    func clear() {
        live = ClusterLive()
    }

    /// Forgets the samples and the key they were for (the screenshot mode is switching): a poll
    /// still running stops recording, and the next one starts afresh.
    func forget() {
        live = ClusterLive()
        key = ""
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
