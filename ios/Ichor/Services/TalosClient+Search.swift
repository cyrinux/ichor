import Foundation
import Ichorgo
import IchorCore

extension TalosClient {
    /// `entries` ranked for `query` by the Go core, best first, at most `limit`. No cluster call:
    /// it reads only the blobs given.
    static func search(entries: [SearchEntry], query: String, limit: Int = 50) async throws -> SearchResults {
        let json = SearchEntries(entries: entries).json
        return try await Self.json { IchorgoSearchSnapshots(json, query, limit, $0) }
    }

    /// The active cluster's `screens` ranked for `query` by the Go core, best first, at most `limit`.
    static func searchScreens(entries screens: [ScreenEntry], query: String, limit: Int = 20) async throws -> ScreenMatches {
        let json = ScreenEntries(screens: screens).json
        return try await Self.json { IchorgoSearchScreens(json, query, limit, $0) }
    }

    /// The built-in demo's overview, pods, workloads, CronJobs and GitOps apps, as search entries.
    static func demoSearchEntries() -> [SearchEntry] {
        (try? TalosJSON.decode(SearchEntries.self, from: IchorgoDemoSearchEntries()).entries) ?? []
    }
}
