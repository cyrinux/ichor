import Foundation

// Mirrors go/ichorgo/kube_certmanager_details.go: what explains a certificate's state.

/// A certificate's conditions, its latest requests (the newest first) with their ACME orders and
/// challenges, the events of all of them (the newest first) and the controller log lines naming
/// them (oldest first).
public struct CertDetails: Decodable, Equatable, Sendable {
    public let conditions: [CertCondition]
    public let requests: [CertRequestDetail]
    public let events: [CertEvent]
    public let log: [String]
    /// What could not be read; the rest is still there.
    public let error: String

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        conditions = try c.field(.conditions, [])
        requests = try c.field(.requests, [])
        events = try c.field(.events, [])
        log = try c.field(.log, [])
        error = try c.field(.error, "")
    }

    private enum CodingKeys: String, CodingKey { case conditions, requests, events, log, error }
}

public struct CertCondition: Decodable, Equatable, Sendable {
    public let type: String
    /// "True", "False" or "Unknown".
    public let status: String
    public let reason: String
    public let message: String
    /// Unix ms of the last transition, 0 when unknown.
    public let time: Int64

    public var isTrue: Bool { status == "True" }

    /// A request denied or invalid is bad; Ready or Approved not true needs a look.
    public var health: ServiceHealth {
        switch type {
        case "Denied", "InvalidRequest": isTrue ? .critical : .ok
        case "Ready", "Approved": isTrue ? .ok : .warning
        default: .ok
        }
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        type = try c.field(.type, "")
        status = try c.field(.status, "")
        reason = try c.field(.reason, "")
        message = try c.field(.message, "")
        time = try c.field(.time, 0)
    }

    private enum CodingKeys: String, CodingKey { case type, status, reason, message, time }
}

public struct CertRequestDetail: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    /// Unix ms.
    public let created: Int64
    public let conditions: [CertCondition]
    public let orders: [AcmeOrder]

    public var id: String { name }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        created = try c.field(.created, 0)
        conditions = try c.field(.conditions, [])
        orders = try c.field(.orders, [])
    }

    private enum CodingKeys: String, CodingKey { case name, created, conditions, orders }
}

public struct AcmeOrder: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    /// pending, ready, valid, invalid, errored…; "" before the first sync.
    public let state: String
    public let reason: String
    public let challenges: [AcmeChallenge]

    public var id: String { name }
    public var failed: Bool { acmeFailed(state) }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        state = try c.field(.state, "")
        reason = try c.field(.reason, "")
        challenges = try c.field(.challenges, [])
    }

    private enum CodingKeys: String, CodingKey { case name, state, reason, challenges }
}

public struct AcmeChallenge: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    /// HTTP-01 or DNS-01.
    public let type: String
    public let dnsName: String
    public let wildcard: Bool
    public let state: String
    /// Why it is not valid yet, e.g. the HTTP status cert-manager's self check got.
    public let reason: String
    public let presented: Bool

    public var id: String { name }
    public var domain: String { (wildcard ? "*." : "") + dnsName }

    /// Failed for good is critical; still waiting with a reason is a warning.
    public var health: ServiceHealth {
        if acmeFailed(state) { return .critical }
        return state != "valid" && !reason.isEmpty ? .warning : .ok
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        type = try c.field(.type, "")
        dnsName = try c.field(.dnsName, "")
        wildcard = try c.field(.wildcard, false)
        state = try c.field(.state, "")
        reason = try c.field(.reason, "")
        presented = try c.field(.presented, false)
    }

    private enum CodingKeys: String, CodingKey { case name, type, dnsName, wildcard, state, reason, presented }
}

public struct CertEvent: Decodable, Equatable, Sendable {
    /// Unix ms of the last occurrence.
    public let time: Int64
    /// Normal or Warning.
    public let type: String
    public let reason: String
    public let message: String
    /// "CertificateRequest/site-1".
    public let object: String
    public let count: Int

    public var warning: Bool { type == "Warning" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        time = try c.field(.time, 0)
        type = try c.field(.type, "")
        reason = try c.field(.reason, "")
        message = try c.field(.message, "")
        object = try c.field(.object, "")
        count = try c.field(.count, 1)
    }

    private enum CodingKeys: String, CodingKey { case time, type, reason, message, object, count }
}

/// An ACME order or challenge state that will not succeed without a change.
public func acmeFailed(_ state: String) -> Bool {
    state == "invalid" || state == "errored" || state == "expired"
}
