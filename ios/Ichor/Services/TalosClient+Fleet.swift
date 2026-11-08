import Foundation
import Ichorgo
import IchorCore

extension TalosClient {
    /// Every cluster's last background check (`entries`) ranked worst first by the Go core, as
    /// of `now`. No cluster call: it reads only the snapshots given.
    static func fleetSummary(entries: [FleetEntry], now: Date) async throws -> FleetSummary {
        let json = FleetEntries(clusters: entries).json
        let millis = Int64((now.timeIntervalSince1970 * 1000).rounded())
        return try await Self.json { IchorgoFleetSummary(json, millis, $0) }
    }

    /// The demo's made-up fleet of five clusters, as of `now`.
    static func demoFleetSummary(now: Date) async throws -> FleetSummary {
        let millis = Int64((now.timeIntervalSince1970 * 1000).rounded())
        let json = try await run { _ in IchorgoDemoFleetSummary(millis) }
        return try TalosJSON.decode(FleetSummary.self, from: json)
    }
}
