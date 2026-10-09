import Foundation
import Ichorgo
import IchorCore

/// The cluster's Alertmanager (os:admin): its alerts and silences, silencing and expiring, reached
/// like a metrics source through the Kubernetes API service proxy or at a URL (see TalosClient
/// for the conventions). Silence and expire are refused in the demo, and recorded in the audit log.
extension TalosClient {
    /// Alertmanagers among the cluster's Services, the likeliest first.
    func alertmanagerDiscover() async throws -> [PromSource] {
        let found: PromDiscovery = try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoAlertmanagerDiscover(config, context, kubeServer, $0)
        }
        return found.sources
    }

    /// The alerts in the states `filter` picks, grouped by alertname, the worst group first.
    func alertmanagerAlerts(_ source: PromSource, filter: AMStateFilter = AMStateFilter()) async throws -> AMAlerts {
        let sourceJSON = try TalosJSON.encode(source)
        return try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoAlertmanagerAlerts(config, context, kubeServer, sourceJSON, filter.active, filter.silenced, filter.inhibited, "", "", $0)
        }
    }

    /// The active and pending silences, the latest expired ones too with `withExpired`.
    func alertmanagerSilences(_ source: PromSource, withExpired: Bool) async throws -> [AMSilence] {
        let sourceJSON = try TalosJSON.encode(source)
        let list: AMSilences = try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoAlertmanagerSilences(config, context, kubeServer, sourceJSON, withExpired, $0)
        }
        return list.silences
    }

    /// Silences `matchers` for `minutes` from now with `comment`; the new silence's ID.
    func alertmanagerSilence(_ source: PromSource, matchers: [AMMatcher], minutes: Int, comment: String) async throws -> String {
        let sourceJSON = try TalosJSON.encode(source)
        let matchersJSON = try TalosJSON.encode(matchers)
        return try await Self.run { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] error -> String in
            IchorgoAlertmanagerSilence(config, context, kubeServer, sourceJSON, matchersJSON, minutes, comment, error)
        }
    }

    /// Ends the silence `id` now.
    func alertmanagerExpire(_ source: PromSource, id: String) async throws {
        let sourceJSON = try TalosJSON.encode(source)
        try await Self.run { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] error -> Void in
            _ = IchorgoAlertmanagerExpire(config, context, kubeServer, sourceJSON, id, error)
        }
    }

    /// The matchers that silence exactly the alert with `labels` (alertname first, the HA replica
    /// labels left out), for the silence form to start from.
    static func alertmanagerSilenceMatchers(_ labels: [String: String]) async throws -> [AMMatcher] {
        let labelsJSON = try TalosJSON.encode(labels)
        return try await json { IchorgoAlertmanagerSilenceMatchers(labelsJSON, $0) }
    }
}
