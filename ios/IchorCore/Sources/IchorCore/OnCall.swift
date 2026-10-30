import Foundation

// Mirrors go/ichorgo/oncall*.go: a cluster's pager, its open incidents and the one behind each
// Alertmanager alert (times in unix ms, 0 when unset).

/// A cluster's on-call integration. The token stays in the Keychain: never logged, never in a backup.
public struct OnCallConfig: Codable, Equatable, Sendable {
    public var kind: String
    public var token: String
    /// The email of the PagerDuty user who acts.
    public var from: String
    public var serviceIds: [String]
    public var region: String

    public static let kindPagerDuty = "pagerduty"
    public static let regionUS = "us"
    public static let regionEU = "eu"

    public init(
        kind: String = OnCallConfig.kindPagerDuty, token: String = "", from: String = "", serviceIds: [String] = [],
        region: String = OnCallConfig.regionUS
    ) {
        self.kind = kind
        self.token = token
        self.from = from
        self.serviceIds = serviceIds
        self.region = region
    }

    /// Enough to ask PagerDuty for incidents.
    public var isSet: Bool {
        !token.trimmingCharacters(in: .whitespaces).isEmpty && !serviceIds.isEmpty
    }
}

public enum OnCallStatus: String, Sendable, WireEnum {
    case triggered, acknowledged, unknown

    public static let wireFallback = OnCallStatus.unknown
}

public enum OnCallVerb: String, Sendable {
    case acknowledge, resolve
}

public enum OnCallConfidence: String, Sendable, WireEnum {
    case exact, alertname

    public static let wireFallback = OnCallConfidence.alertname
}

public struct OnCallIncident: Decodable, Hashable, Identifiable, Sendable {
    public let id: String
    public let number: Int
    public let title: String
    public let status: OnCallStatus
    public let urgency: String
    public let createdAt: Int64
    public let url: String
    public let service: String
    public let assignees: [String]
    public let acknowledgedBy: String
    public let matchedFingerprints: [String]

    public init(
        id: String = "", number: Int = 0, title: String = "", status: OnCallStatus = .unknown, urgency: String = "",
        createdAt: Int64 = 0, url: String = "", service: String = "", assignees: [String] = [],
        acknowledgedBy: String = "", matchedFingerprints: [String] = []
    ) {
        self.id = id
        self.number = number
        self.title = title
        self.status = status
        self.urgency = urgency
        self.createdAt = createdAt
        self.url = url
        self.service = service
        self.assignees = assignees
        self.acknowledgedBy = acknowledgedBy
        self.matchedFingerprints = matchedFingerprints
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            id: try c.field(.id, ""), number: try c.field(.number, 0), title: try c.field(.title, ""),
            status: try c.wire(.status), urgency: try c.field(.urgency, ""),
            createdAt: try c.field(.createdAt, 0), url: try c.field(.url, ""), service: try c.field(.service, ""),
            assignees: try c.field(.assignees, []), acknowledgedBy: try c.field(.acknowledgedBy, ""),
            matchedFingerprints: try c.field(.matchedFingerprints, []))
    }

    private enum CodingKeys: String, CodingKey {
        case id, number, title, status, urgency, createdAt, url, service, assignees, acknowledgedBy, matchedFingerprints
    }

    /// Who acknowledged it, whoever it is assigned to otherwise; "" when nobody is known.
    public var acknowledgerName: String { acknowledgedBy.isEmpty ? (assignees.first ?? "") : acknowledgedBy }
}

public struct OnCallMatch: Decodable, Hashable, Sendable {
    public let incidentId: String
    public let status: OnCallStatus
    public let confidence: OnCallConfidence

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        incidentId = try c.field(.incidentId, "")
        status = try c.wire(.status)
        confidence = try c.wire(.confidence)
    }

    private enum CodingKeys: String, CodingKey { case incidentId, status, confidence }
}

/// The open incidents, and the one behind each alert by its fingerprint.
public struct OnCallIncidents: Decodable, Hashable, Sendable {
    public let incidents: [OnCallIncident]
    public let matches: [String: OnCallMatch]

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        incidents = try c.field(.incidents, [])
        matches = try c.field(.matches, [:])
    }

    private enum CodingKeys: String, CodingKey { case incidents, matches }

    /// The incident behind the alert of `fingerprint`, nil when none.
    public func incident(of fingerprint: String) -> OnCallIncident? {
        guard let match = matches[fingerprint] else { return nil }
        return incidents.first { $0.id == match.incidentId }
    }
}

/// What Test answers: `user` is empty for an account-level token.
public struct OnCallIdentity: Decodable, Hashable, Sendable {
    public let user: String
    public let account: String

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        user = try c.field(.user, "")
        account = try c.field(.account, "")
    }

    private enum CodingKeys: String, CodingKey { case user, account }
}

/// What the matching reads of an alert.
public struct OnCallAlertInput: Encodable, Sendable {
    public let fingerprint: String
    public let labels: [String: String]

    public init(fingerprint: String, labels: [String: String]) {
        self.fingerprint = fingerprint
        self.labels = labels
    }
}

/// The on-call badge of an alert row: what the app writes for an incident's status.
public enum OnCallBadge: Equatable, Sendable {
    case triggered
    case acknowledged(by: String)
    case hidden

    public init(_ incident: OnCallIncident) {
        switch incident.status {
        case .triggered: self = .triggered
        case .acknowledged: self = .acknowledged(by: incident.acknowledgerName)
        case .unknown: self = .hidden
        }
    }
}
