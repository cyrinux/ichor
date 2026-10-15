import Foundation

// Data services: what the phone's own resolver answers for ExternalDNS's hostnames
// (see ExternalDns.swift). Mirrors go/ichorgo/dns_check.go.

/// One hostname to check, as the Go core reads it: the record ExternalDNS is asked to publish,
/// plus the covering controller's owner id and TXT prefix for the ownership check.
public struct DnsCheckRequest: Codable, Equatable, Sendable {
    public let dnsName: String
    /// A, AAAA or CNAME.
    public let recordType: String
    public let targets: [String]
    public let ownerId: String
    public let txtPrefix: String

    public init(dnsName: String, recordType: String, targets: [String], ownerId: String = "", txtPrefix: String = "") {
        self.dnsName = dnsName
        self.recordType = recordType
        self.targets = targets
        self.ownerId = ownerId
        self.txtPrefix = txtPrefix
    }
}

/// What resolving a hostname from the phone found, as the Go core names it. A status a newer
/// core names reads as no verdict: the row shows nothing rather than a wrong verdict.
public enum DnsCheckStatus: String, Sendable {
    /// An answer is one of the record's targets.
    case match
    /// It resolves, but to something else: split-horizon DNS at home, propagation, another owner.
    case mismatch
    /// It does not resolve at all.
    case missing
    /// The resolver could not answer.
    case error
}

/// One hostname's verdict. `ownerMatch` is nil when no TXT registry record was readable, so
/// the app says "owner unknown" rather than "not ours".
public struct DnsCheckEntry: Decodable, Equatable, Sendable {
    public let dnsName: String
    /// The wire value of `DnsCheckStatus`.
    public let status: String
    public let answers: [String]
    /// The ExternalDNS owner id the TXT record names, "" when none was read.
    public let owner: String
    /// nil when no TXT registry record was readable.
    public let ownerMatch: Bool?
    /// How long the lookup took, in milliseconds.
    public let ms: Int
    /// Why it could not be checked, "" otherwise.
    public let message: String

    public var verdict: DnsCheckStatus? { DnsCheckStatus(rawValue: status) }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        dnsName = try c.field(.dnsName, "")
        status = try c.field(.status, "")
        answers = try c.field(.answers, [])
        owner = try c.field(.owner, "")
        ownerMatch = try c.decodeIfPresent(Bool.self, forKey: .ownerMatch)
        ms = try c.field(.ms, 0)
        message = try c.field(.message, "")
    }

    private enum CodingKeys: String, CodingKey {
        case dnsName, status, answers, owner, ownerMatch, ms, message
    }
}

/// The whole check's answer: one entry per record, in the order they were sent.
public struct DnsCheckResult: Decodable, Equatable, Sendable {
    public let results: [DnsCheckEntry]
    /// "system" or "demo".
    public let resolver: String

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        results = try c.field(.results, [])
        resolver = try c.field(.resolver, "")
    }

    private enum CodingKeys: String, CodingKey { case results, resolver }
}
