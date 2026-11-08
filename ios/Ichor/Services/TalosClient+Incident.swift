import Foundation
import Ichorgo
import IchorCore

/// The incident summary (CYR-77): Go writes the Markdown from the history ring (unsealed by
/// HistoryStore) and the action log. Local, no cluster call; in screenshot mode it comes back
/// masked like every result, and `anonymize` adds its own stronger pass.
extension TalosClient {
    /// `context`'s summary between `since` and `now` (epoch ms). `ring` nil: no recorded history
    /// yet, which Go answers with a summary saying so.
    static func incidentSummary(context: String, ring: Data?, since: Int64, now: Int64, anonymize: Bool) async throws
        -> String
    {
        try await run { IchorgoIncidentSummary(context, ring, since, now, anonymize, $0) }
    }
}
