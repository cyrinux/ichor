import Foundation
import Ichorgo
import IchorCore

extension TalosClient {
    /// What the alert rules do with a run's `alerts` at `now`, decided by the Go core
    /// (AlertRulesApply): no cluster call and no state. Quiet hours without a zone are in the
    /// phone's (`TimeZone.current`). Throws when Go refuses the rules (a bad stored time or
    /// zone): the caller then posts as before the rules.
    static func decideAlerts(rules: AlertRules, alerts: [AlertInput], now: Date) async throws -> AlertDecisions {
        let request = try alertRulesRequest(rules: rules, alerts: alerts, zone: TimeZone.current.identifier)
        let millis = Int64((now.timeIntervalSince1970 * 1000).rounded())
        return try await Self.json { IchorgoAlertRulesApply(request.rules, request.alerts, millis, $0) }
    }
}
