import Foundation

// Mirrors go/ichorgo/nettools.go and nettools_parse.go (StartNodeNetTool).

/// The checks a node can run on its network; the raw value is StartNodeNetTool's tool.
public enum NetTool: String, CaseIterable, Identifiable, Sendable {
    case dns, ping, port, trace, http

    public var id: String { rawValue }
}

public struct NetDnsRecord: Decodable, Equatable, Sendable {
    public let name: String
    public let type: String
    public let ttl: Int
    public let value: String

    private enum CodingKeys: String, CodingKey { case name, type, ttl, value }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        type = try c.field(.type, "")
        ttl = try c.field(.ttl, 0)
        value = try c.field(.value, "")
    }
}

public struct NetDnsResult: Decodable, Equatable, Sendable {
    /// The answer's status (NOERROR, NXDOMAIN…), empty when dig got none.
    public let status: String
    public let records: [NetDnsRecord]
    public let server: String
    public let queryMs: Int

    private enum CodingKeys: String, CodingKey { case status, records, server, queryMs }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        status = try c.field(.status, "")
        records = try c.field(.records, [])
        server = try c.field(.server, "")
        queryMs = try c.field(.queryMs, 0)
    }
}

public struct NetPingResult: Decodable, Equatable, Sendable {
    public let sent: Int
    public let received: Int
    public let lossPct: Double
    public let minMs: Double
    public let avgMs: Double
    public let maxMs: Double

    private enum CodingKeys: String, CodingKey { case sent, received, lossPct, minMs, avgMs, maxMs }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        sent = try c.field(.sent, 0)
        received = try c.field(.received, 0)
        lossPct = try c.field(.lossPct, 0)
        minMs = try c.field(.minMs, 0)
        avgMs = try c.field(.avgMs, 0)
        maxMs = try c.field(.maxMs, 0)
    }
}

public struct NetPortResult: Decodable, Equatable, Sendable {
    public let open: Bool
    public let message: String

    private enum CodingKeys: String, CodingKey { case open, message }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        open = try c.field(.open, false)
        message = try c.field(.message, "")
    }
}

/// A hop of the path; `host` is "???" when it did not answer.
public struct NetTraceHop: Decodable, Equatable, Identifiable, Sendable {
    public let hop: Int
    public let host: String
    public let lossPct: Double
    public let avgMs: Double

    public var id: Int { hop }
    public var silent: Bool { host == "???" }

    private enum CodingKeys: String, CodingKey { case hop, host, lossPct, avgMs }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        hop = try c.field(.hop, 0)
        host = try c.field(.host, "")
        lossPct = try c.field(.lossPct, 0)
        avgMs = try c.field(.avgMs, 0)
    }
}

public struct NetHttpResult: Decodable, Equatable, Sendable {
    /// The final status after redirects; 0: no answer.
    public let status: Int
    public let redirectUrl: String
    public let totalMs: Double
    /// An https URL; `tlsOk` then says whether its certificate verified.
    public let tls: Bool
    public let tlsOk: Bool
    public let subject: String
    public let issuer: String
    public let notBefore: String
    public let notAfter: String
    public let daysLeft: Int
    public let error: String

    private enum CodingKeys: String, CodingKey {
        case status, redirectUrl, totalMs, tls, tlsOk, subject, issuer, notBefore, notAfter, daysLeft, error
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        status = try c.field(.status, 0)
        redirectUrl = try c.field(.redirectUrl, "")
        totalMs = try c.field(.totalMs, 0)
        tls = try c.field(.tls, false)
        tlsOk = try c.field(.tlsOk, false)
        subject = try c.field(.subject, "")
        issuer = try c.field(.issuer, "")
        notBefore = try c.field(.notBefore, "")
        notAfter = try c.field(.notAfter, "")
        daysLeft = try c.field(.daysLeft, 0)
        error = try c.field(.error, "")
    }
}

/// A check's parsed run: only the tool's own section is set; `raw` is the full output.
public struct NetToolResult: Decodable, Equatable, Sendable {
    public let tool: String
    public let target: String
    public let ok: Bool
    public let dns: NetDnsResult?
    public let ping: NetPingResult?
    public let port: NetPortResult?
    public let trace: [NetTraceHop]?
    public let http: NetHttpResult?
    public let raw: String

    private enum CodingKeys: String, CodingKey { case tool, target, ok, dns, ping, port, trace, http, raw }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        tool = try c.field(.tool, "")
        target = try c.field(.target, "")
        ok = try c.field(.ok, false)
        dns = try c.decodeIfPresent(NetDnsResult.self, forKey: .dns)
        ping = try c.decodeIfPresent(NetPingResult.self, forKey: .ping)
        port = try c.decodeIfPresent(NetPortResult.self, forKey: .port)
        trace = try c.decodeIfPresent([NetTraceHop].self, forKey: .trace)
        http = try c.decodeIfPresent(NetHttpResult.self, forKey: .http)
        raw = try c.field(.raw, "")
    }
}

/// The DNS record types offered (the core's list).
public let netDnsRecords = ["A", "AAAA", "CNAME", "MX", "NS", "PTR", "SRV", "TXT"]

public enum NetTargetProblem: Equatable, Sendable {
    case empty, characters, hostPort, url, host
}

/// Why `target` cannot be given to `tool`, checked in the field before running, or nil when it
/// looks right. The core checks it again: this only saves a round trip.
public func netTargetProblem(_ tool: NetTool, _ target: String) -> NetTargetProblem? {
    let t = target.trimmingCharacters(in: .whitespacesAndNewlines)
    let forbidden = Set(" \t\r\n;|&$`<>(){}'\"\\*!")
    if t.isEmpty { return .empty }
    if t.hasPrefix("-") || t.contains(where: { forbidden.contains($0) }) { return .characters }
    switch tool {
    case .port:
        return t.range(of: #"^(\[[0-9a-fA-F:.]+\]|[^:\[\]]+):[0-9]{1,5}$"#, options: .regularExpression) == nil ? .hostPort : nil
    case .http:
        return t.hasPrefix("http://") || t.hasPrefix("https://") ? nil : .url
    case .dns, .ping, .trace:
        return t.contains("/") ? .host : nil
    }
}

/// The last targets used, newest first, at most `max`, `target` moved to the front.
public func rememberNetTarget(_ recent: [String], _ target: String, max: Int = 10) -> [String] {
    let t = target.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !t.isEmpty else { return Array(recent.prefix(max)) }
    return Array(([t] + recent.filter { $0 != t }).prefix(max))
}
