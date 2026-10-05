import Foundation
import IchorCore

extension AppModel {
    /// The cluster whose nodes Wake-on-LAN applies to; nil in screenshot mode (node addresses
    /// are masked then and would not match the saved ones, and MACs are revealing).
    var wakeOnLanFingerprint: String? {
        guard !privacyMask, let fingerprint = activeSummary?.fingerprint, !fingerprint.isEmpty else { return nil }
        return fingerprint
    }

    /// Records the MACs of the reachable `nodes`, once per node while the app runs, so a node
    /// that goes down before anyone set up its Wake-on-LAN can still be woken.
    func recordNodeMacs(of nodes: [NodeOverview]) async {
        guard let fingerprint = wakeOnLanFingerprint, let client else { return }
        let store = WakeOnLanStore.shared
        for node in nodes where node.reachable && store.shouldRecord(fingerprint: fingerprint, node: node.node) {
            // Unreadable now: tried again on a later load.
            guard let network = try? await client.network(node: node.node), wakeOnLanFingerprint == fingerprint else { continue }
            store.record(IchorCore.seenMacs(network.links), fingerprint: fingerprint, node: node.node)
        }
    }
}
