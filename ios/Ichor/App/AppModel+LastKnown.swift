import Foundation
import IchorCore

/// Where a fetch's result may be kept: the cluster it came from, and the screenshot mode
/// generation it was fetched under (a result of the previous one is not kept).
struct LastKnownTarget {
    let fingerprint: String
    let generation: Int
}

/// The last known state (keepLastKnown): what a screen with nothing in memory shows at once,
/// while it loads, and what each successful fetch leaves for the next time.
extension AppModel {
    /// The active cluster's, nil when the setting is off and for the demo (never stored).
    var lastKnownTarget: LastKnownTarget? {
        guard keepLastKnown, let context = activeSummary, !context.demo, !context.fingerprint.isEmpty else { return nil }
        return LastKnownTarget(fingerprint: context.fingerprint, generation: dataGeneration)
    }

    /// The stored value of `domain` for the active cluster, if any (less than a day old).
    func lastKnown<T: Decodable>(_ domain: LastKnownDomain, as type: T.Type = T.self) -> (value: T, at: Date)? {
        guard let target = lastKnownTarget,
              let entry = LastKnownStore.read(fingerprint: target.fingerprint, key: domain.key),
              let value = try? TalosJSON.decode(T.self, from: entry.json) else { return nil }
        return (value, entry.date)
    }

    /// `state`, or the stored value of `domain` while it has nothing to show.
    func seeded<T: Decodable>(_ state: LoadState<T>, from domain: LastKnownDomain) -> LoadState<T> {
        seeded(state, from: domain, as: T.self) { $0 }
    }

    /// Same, for a screen showing part of what was fetched (`show` takes it out).
    func seeded<Stored: Decodable, T>(_ state: LoadState<T>, from domain: LastKnownDomain, as type: Stored.Type,
                                      _ show: (Stored) -> T) -> LoadState<T> {
        guard case .loading = state, let known = lastKnown(domain, as: Stored.self) else { return state }
        return .loaded(show(known.value), at: known.at)
    }

    /// `domain` fetched with `client`, kept as last known when the setting is on.
    func fetch<T: Decodable & Sendable>(_ domain: LastKnownDomain, as type: T.Type = T.self, with client: TalosClient) async throws -> T {
        let target = lastKnownTarget
        let fetched: (value: T, json: String) = try await client.fetch(domain)
        if let target { remember(domain, json: fetched.json, at: Date(), for: target) }
        return fetched.value
    }

    /// Stores `json` unless the setting was turned off or the screenshot mode changed meanwhile.
    /// Best effort: a write that fails (device locked) leaves the previous entry.
    func remember(_ domain: LastKnownDomain, json: String, at: Date, for target: LastKnownTarget) {
        guard keepLastKnown, target.generation == dataGeneration else { return }
        try? LastKnownStore.save(fingerprint: target.fingerprint, entry: LastKnownEntry(key: domain.key, at: at, json: json))
    }
}
