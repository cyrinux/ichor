import Foundation

/// The first thing to try when no node answers.
public enum RecoveryAction: Sendable {
    case vpnSettings, signIn, replaceConfig, retry
}

/// What the primary action will and will not do, said next to it.
public enum RecoveryExplanation: Sendable {
    case omniSession, replaceConfig
}

/// What the outage notice and banner offer, from why no node answered.
public struct OutageRecovery: Equatable, Sendable {
    public let primary: RecoveryAction
    /// Retry beside the primary action; false when Retry is the primary one.
    public let retrySecondary: Bool
    public let scan: Bool
    public let editEndpoints: Bool
    public let explanation: RecoveryExplanation?

    public init(
        primary: RecoveryAction, retrySecondary: Bool, scan: Bool, editEndpoints: Bool,
        explanation: RecoveryExplanation?
    ) {
        self.primary = primary
        self.retrySecondary = retrySecondary
        self.scan = scan
        self.editEndpoints = editEndpoints
        self.explanation = explanation
    }
}

/// How Go's x509 error names a certificate for another host: the endpoint is the thing to change.
private let wrongHostCertificate = "certificate is valid for"

/// Retry cannot repair refused credentials: they get a way to replace them. A refusal means a node
/// answered, so its address is right, unless the certificate names another host. The demo has no
/// credentials or endpoints to change, and a masked screen shows no endpoints. Android's
/// outageRecovery (model/OutageRecovery.kt) makes the same choice.
public func outageRecovery(_ outage: ClusterOutage, omni: Bool, demo: Bool, masked: Bool) -> OutageRecovery {
    let editable = !demo && !masked
    switch outage.cause {
    case .network:
        return OutageRecovery(
            primary: .vpnSettings, retrySecondary: true, scan: !demo, editEndpoints: editable, explanation: nil)
    case .credentials:
        let wrongAddress = editable && outage.errors.contains { $0.contains(wrongHostCertificate) }
        if demo {
            return OutageRecovery(
                primary: .retry, retrySecondary: false, scan: false, editEndpoints: false, explanation: nil)
        }
        if omni {
            return OutageRecovery(
                primary: .signIn, retrySecondary: true, scan: false, editEndpoints: wrongAddress,
                explanation: .omniSession)
        }
        return OutageRecovery(
            primary: .replaceConfig, retrySecondary: true, scan: false, editEndpoints: wrongAddress,
            explanation: .replaceConfig)
    case .other:
        return OutageRecovery(
            primary: .retry, retrySecondary: false, scan: false, editEndpoints: editable, explanation: nil)
    }
}
