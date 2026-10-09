import Foundation

// Data services: cert-manager certificates and issuers (see DataServices.swift).

public struct CertManagerStatus: Decodable, Equatable, Sendable {
    public let version: String
    public let error: String
    /// Worst first, then the soonest expiry.
    public let certificates: [Certificate]
    public let issuers: [CertIssuer]

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        version = try c.field(.version, "")
        error = try c.field(.error, "")
        certificates = try c.field(.certificates, [])
        issuers = try c.field(.issuers, [])
    }

    private enum CodingKeys: String, CodingKey { case version, error, certificates, issuers }
}

public struct Certificate: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    public let secretName: String
    /// The common name then the DNS names, the first few; dnsNameCount counts them all.
    public let dnsNames: [String]
    public let dnsNameCount: Int
    /// "ClusterIssuer/letsencrypt", "Issuer/internal-ca".
    public let issuer: String
    public let health: ServiceHealth
    /// Known reasons only; values from newer cores are dropped.
    public let reasons: [CertReason]
    public let ready: Bool
    /// cert-manager is issuing it now (a renewal, or one forced from the app).
    public let issuing: Bool
    /// The Ready condition's message when not ready.
    public let message: String
    /// Unix ms, 0 before the first issuance.
    public let notAfter: Int64
    /// Unix ms, 0 when no renewal is planned.
    public let renewalTime: Int64
    public let failedAttempts: Int

    public var id: String { label }
    public var label: String { "\(namespace)/\(name)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        secretName = try c.field(.secretName, "")
        dnsNames = try c.field(.dnsNames, [])
        dnsNameCount = try c.field(.dnsNameCount, 0)
        issuer = try c.field(.issuer, "")
        health = try c.wire(.health)
        reasons = try c.wireList(.reasons)
        ready = try c.field(.ready, false)
        issuing = try c.field(.issuing, false)
        message = try c.field(.message, "")
        notAfter = try c.field(.notAfter, 0)
        renewalTime = try c.field(.renewalTime, 0)
        failedAttempts = try c.field(.failedAttempts, 0)
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, secretName, dnsNames, dnsNameCount, issuer, health, reasons, ready, issuing, message, notAfter, renewalTime, failedAttempts
    }
}

public struct CertIssuer: Decodable, Equatable, Identifiable, Sendable {
    /// Issuer or ClusterIssuer.
    public let kind: String
    /// "" for a ClusterIssuer.
    public let namespace: String
    public let name: String
    /// acme, ca, selfSigned, vault or venafi; "" for another.
    public let type: String
    /// The ACME server's host.
    public let server: String
    public let ready: Bool
    public let message: String
    public let health: ServiceHealth

    public var id: String { label }
    /// "ClusterIssuer/letsencrypt", "Issuer/app/internal-ca": unique across both kinds.
    public var label: String { [kind, namespace, name].filter { !$0.isEmpty }.joined(separator: "/") }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.field(.kind, "")
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        type = try c.field(.type, "")
        server = try c.field(.server, "")
        ready = try c.field(.ready, false)
        message = try c.field(.message, "")
        health = try c.wire(.health)
    }

    private enum CodingKeys: String, CodingKey { case kind, namespace, name, type, server, ready, message, health }
}

/// Why a certificate is not ok, as the Go core names it.
public enum CertReason: String, Sendable {
    case expired, expiring, renewalOverdue, notReady, issuer
}
