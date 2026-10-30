import Foundation
import Ichorgo
import IchorCore

/// The cluster's pager (see TalosClient for the conventions). The settings come from OnCallStore on
/// every call; acting is refused in the demo, and recorded in the audit log.
extension TalosClient {
    /// The open incidents of `config`, matched to `alerts`. The demo answers without settings.
    func onCallIncidents(_ config: OnCallConfig, alerts: AMAlerts) async throws -> OnCallIncidents {
        let configJSON = try TalosJSON.encode(config)
        let inputs = alerts.alerts.map { OnCallAlertInput(fingerprint: $0.fingerprint, labels: $0.labels) }
        let alertsJSON = try TalosJSON.encode(inputs)
        return try await Self.json { [config = self.kubeConfig, context = self.kubeContext] in
            IchorgoOnCallIncidents(config, context, configJSON, alertsJSON, $0)
        }
    }

    /// Acknowledges or resolves the incident `id`.
    func onCallAct(_ config: OnCallConfig, id: String, verb: OnCallVerb) async throws {
        let configJSON = try TalosJSON.encode(config)
        let verbName = verb.rawValue
        try await Self.run { [config = self.kubeConfig, context = self.kubeContext] error -> Void in
            _ = IchorgoOnCallAct(config, context, configJSON, id, verbName, error)
        }
    }

    /// Who `config`'s token is; throws when PagerDuty refuses it.
    static func onCallTest(_ config: OnCallConfig) async throws -> OnCallIdentity {
        let configJSON = try TalosJSON.encode(config)
        return try await json { IchorgoOnCallTest(configJSON, $0) }
    }
}
