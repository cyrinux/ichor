import Foundation
import Ichorgo
import IchorCore

/// The history ring (CYR-39): Go keeps the format, HistoryStore seals the bytes. Local calls, no
/// cluster: in screenshot mode the query and the summary come back masked like every result.
extension TalosClient {
    /// `ring` (nil: none yet) with `record` (HistoryRecord JSON) added at `now` (epoch ms).
    /// Throws for an invalid record or a ring from a newer version: the stored one is kept then.
    /// Quick and local, so called in place (the background run is already off the main actor).
    static func historyAppend(ring: Data?, record: String, now: Int64) throws -> Data {
        var error: NSError?
        let next = IchorgoHistoryAppend(ring, record, now, &error)
        if let error { throw TalosError(message: error.localizedDescription) }
        return next ?? Data()
    }

    /// Node uptime, alert spans, series and gaps between `since` and `now` (epoch ms).
    static func historyQuery(ring: Data?, since: Int64, now: Int64) async throws -> HistoryQueryResult {
        try await json { IchorgoHistoryQuery(ring, since, now, $0) }
    }

    /// What happened after `lastLooked` (epoch ms).
    static func historySince(ring: Data?, lastLooked: Int64) async throws -> HistorySinceSummary {
        try await json { IchorgoHistorySince(ring, lastLooked, $0) }
    }

    /// The ring as anonymised JSON (nodes, volumes and alerts renamed), for the support bundle.
    static func historyExport(ring: Data?) async throws -> String {
        try await run { IchorgoHistoryExport(ring, $0) }
    }
}
