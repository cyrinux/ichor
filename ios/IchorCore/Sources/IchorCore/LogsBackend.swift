import Foundation

/// Log backends the app can search (PromSource.kind).
public let logsKindLoki = "loki"
public let logsKindVictoriaLogs = "victorialogs"

/// The range choices, in hours back from now (the last is 7 days).
public let logSearchHours = [1, 6, 24, 168]
public let logSearchDefaultHours = 24
/// 31 days: the longest range the Go core accepts.
public let logSearchMaxHours = 744
public let logSearchMaxSaved = 20

/// One log backend found among the cluster's Services.
public struct LogsCandidate: Decodable, Equatable, Sendable {
    public let kind: String
    public let source: PromSource

    private enum CodingKeys: String, CodingKey { case kind, source }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.field(.kind, "")
        source = try c.field(.source, PromSource())
    }
}

/// What LogsDiscover answers: the log backends among the Services, the likeliest first.
public struct LogsDiscovery: Decodable, Equatable, Sendable {
    public let candidates: [LogsCandidate]

    private enum CodingKeys: String, CodingKey { case candidates }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        candidates = try c.field(.candidates, [])
    }
}

/// A cluster's log backend (nil: the likeliest one found) and its saved queries, newest first.
public struct LogsBackendConfig: Codable, Equatable, Sendable {
    public var source: PromSource?
    public var saved: [String]

    public init(source: PromSource? = nil, saved: [String] = []) {
        self.source = source
        self.saved = saved
    }

    private enum CodingKeys: String, CodingKey { case source, saved }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        source = try c.decodeIfPresent(PromSource.self, forKey: .source)
        saved = try c.field(.saved, [])
    }
}

/// One line of a search; `time` in unix ms.
public struct LogsLine: Decodable, Equatable, Sendable {
    public let time: Int64
    public let labels: [String: String]
    public let text: String

    private enum CodingKeys: String, CodingKey { case time, labels, text }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        time = try c.field(.time, 0)
        labels = try c.field(.labels, [:])
        text = try c.field(.text, "")
    }

    /// The pod the line came from, under its usual label names.
    public var pod: String? { labels["pod"] ?? labels["kubernetes_pod_name"] }
}

public struct LogsStats: Decodable, Equatable, Sendable {
    public let returned: Int
    public let truncated: Bool

    private enum CodingKeys: String, CodingKey { case returned, truncated }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        returned = try c.field(.returned, 0)
        truncated = try c.field(.truncated, false)
    }
}

/// A search's lines, newest first.
public struct LogsResult: Decodable, Equatable, Sendable {
    public let lines: [LogsLine]
    public let stats: LogsStats?

    private enum CodingKeys: String, CodingKey { case lines, stats }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        lines = try c.field(.lines, [])
        stats = try c.decodeIfPresent(LogsStats.self, forKey: .stats)
    }

    /// The lines as text, oldest first as in a terminal, for Share.
    public var asText: String {
        lines.reversed().map(\.text).joined(separator: "\n")
    }
}

/// What LogsLabels answers: label names, or the values of one.
public struct LogsValues: Decodable, Equatable, Sendable {
    public let values: [String]

    private enum CodingKeys: String, CodingKey { case values }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        values = try c.field(.values, [])
    }
}

/// `saved` with `query` at the top, once, keeping the newest `logSearchMaxSaved`.
public func logSearchSaved(_ saved: [String], adding query: String) -> [String] {
    let trimmed = query.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !trimmed.isEmpty else { return saved }
    return Array(([trimmed] + saved.filter { $0 != trimmed }).prefix(logSearchMaxSaved))
}

/// Adds the matcher `label` = `value` to `query`, in the language of the backend `kind`.
public func logSearchQuery(kind: String, _ query: String, label: String, value: String) -> String {
    let escaped = value.replacingOccurrences(of: "\\", with: "\\\\").replacingOccurrences(of: "\"", with: "\\\"")
    let current = query.trimmingCharacters(in: .whitespacesAndNewlines)
    if kind == logsKindVictoriaLogs {
        let matcher = "\(label):\"\(escaped)\""
        return current.isEmpty ? matcher : "\(current) AND \(matcher)"
    }
    let matcher = "\(label)=\"\(escaped)\""
    guard !current.isEmpty else { return "{\(matcher)}" }
    guard let close = current.firstIndex(of: "}") else { return "{\(matcher)} \(current)" }
    let head = current[..<close].trimmingCharacters(in: .whitespaces)
    let separator = head.hasSuffix("{") ? "" : ","
    return head + separator + matcher + current[close...]
}
