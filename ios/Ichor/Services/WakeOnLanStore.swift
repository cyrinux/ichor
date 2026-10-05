import Foundation
import IchorCore
import Observation
import Security

/// How to wake each node (see WolTarget), by cluster fingerprint and node address (wolKey): the
/// address is what the talosconfig names a node by, and all the app knows of one that is off.
/// Also the MACs each node was last seen with, recorded while it was up, so one that goes down
/// before it was set up can still be woken. Only on this device, in the Keychain (node
/// addresses and MACs map the cluster's network); forgotten with the cluster.
@Observable
@MainActor
final class WakeOnLanStore {
    static let shared = WakeOnLanStore()

    private static let account = "wake-on-lan"

    private struct Stored: Codable {
        var targets: [String: String] = [:]
        var seen: [String: String] = [:]
    }

    private(set) var targets: [String: WolTarget] = [:]
    private(set) var seen: [String: [SeenMac]] = [:]
    /// Nodes whose MACs were already looked at since the app started: once is enough.
    @ObservationIgnored private var looked: Set<String> = []
    @ObservationIgnored private var loaded = false

    private init() {}

    /// Reads the stored settings, once: called when the app loads its config (the Keychain is
    /// unreadable before the first unlock), and before any change, so one never overwrites them.
    func loadIfNeeded() {
        guard !loaded else { return }
        guard let data = Keychain.read(Self.account) else {
            loaded = true
            return
        }
        loaded = true
        guard let stored = try? JSONDecoder().decode(Stored.self, from: data) else { return }
        targets = stored.targets.compactMapValues(decodeWolTarget)
        seen = stored.seen.mapValues(decodeSeenMacs).filter { !$0.value.isEmpty }
    }

    func target(fingerprint: String, node: String) -> WolTarget? {
        targets[wolKey(fingerprint: fingerprint, node: node)]
    }

    func seenMacs(fingerprint: String, node: String) -> [SeenMac] {
        seen[wolKey(fingerprint: fingerprint, node: node)] ?? []
    }

    /// Where "Wake" sends magic packets for `node` (see wakeTargets); empty: nothing known yet.
    func wakeTargets(fingerprint: String, node: String) -> [WolTarget] {
        IchorCore.wakeTargets(saved: target(fingerprint: fingerprint, node: node), seen: seenMacs(fingerprint: fingerprint, node: node))
    }

    /// Saves how to wake `node`; nil forgets it.
    func set(_ target: WolTarget?, fingerprint: String, node: String) {
        guard !fingerprint.isEmpty, !node.isEmpty else { return }
        loadIfNeeded()
        var updated = targets
        updated[wolKey(fingerprint: fingerprint, node: node)] = target
        store(targets: updated, seen: seen)
    }

    /// Whether `node`'s MACs are still to be read since the app started.
    func shouldRecord(fingerprint: String, node: String) -> Bool {
        !fingerprint.isEmpty && !node.isEmpty && !looked.contains(wolKey(fingerprint: fingerprint, node: node))
    }

    /// Remembers the MACs `node` has now; an empty list (nothing physical seen) keeps the previous ones.
    func record(_ macs: [SeenMac], fingerprint: String, node: String) {
        guard !fingerprint.isEmpty, !node.isEmpty else { return }
        loadIfNeeded()
        let key = wolKey(fingerprint: fingerprint, node: node)
        looked.insert(key)
        guard !macs.isEmpty else { return }
        var updated = seen
        updated[key] = macs
        store(targets: targets, seen: updated)
    }

    /// Forgets the nodes of the clusters no longer in `fingerprints`.
    func keep(fingerprints: [String]) {
        loadIfNeeded()
        store(targets: keepWolTargets(targets, fingerprints: fingerprints), seen: keepWolTargets(seen, fingerprints: fingerprints))
    }

    /// Restored from a backup: replaces every setting (the config was replaced too).
    func restore(_ restored: [String: WolTarget]) {
        loadIfNeeded()
        store(targets: restored, seen: seen)
    }

    /// Everything, for a backup (by wolKey).
    var allTargets: [String: WolTarget] { targets }

    func wipe() {
        targets = [:]
        seen = [:]
        looked = []
        Keychain.delete(Self.account)
    }

    /// Best effort: with the Keychain unavailable, the change only lasts until the app stops.
    private func store(targets newTargets: [String: WolTarget], seen newSeen: [String: [SeenMac]]) {
        guard newTargets != targets || newSeen != seen else { return }
        targets = newTargets
        seen = newSeen
        let stored = Stored(targets: newTargets.mapValues(encodeWolTarget), seen: newSeen.mapValues(encodeSeenMacs))
        if stored.targets.isEmpty && stored.seen.isEmpty {
            Keychain.delete(Self.account)
        } else if let data = try? JSONEncoder().encode(stored) {
            try? Keychain.write(data, account: Self.account, accessible: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly)
        }
    }
}
