import Foundation

// Mirrors go/ichorgo/prom*.go: PromQL panels from Prometheus, Mimir, Thanos or VictoriaMetrics.

/// Where PromQL queries go (normalizePromSource): `proxy` reaches a Service through the
/// Kubernetes API, `url` a server the phone reaches itself. `secret` is the bearer token or
/// basic password; the app keeps the whole source in the Keychain.
public struct PromSource: Codable, Equatable, Hashable, Sendable, CustomStringConvertible {
    public static let proxy = "proxy"
    public static let url = "url"
    public static let authNone = ""
    public static let authBearer = "bearer"
    public static let authBasic = "basic"

    public var mode: String
    public var kind: String
    public var namespace: String
    public var service: String
    public var port: Int
    public var pathPrefix: String
    public var url: String
    public var auth: String
    public var username: String
    public var secret: String
    public var ca: String
    public var insecureSkipVerify: Bool
    public var tenant: String

    public init(mode: String = PromSource.proxy, kind: String = "", namespace: String = "", service: String = "", port: Int = 0,
                pathPrefix: String = "", url: String = "", auth: String = PromSource.authNone, username: String = "", secret: String = "",
                ca: String = "", insecureSkipVerify: Bool = false, tenant: String = "") {
        self.mode = mode
        self.kind = kind
        self.namespace = namespace
        self.service = service
        self.port = port
        self.pathPrefix = pathPrefix
        self.url = url
        self.auth = auth
        self.username = username
        self.secret = secret
        self.ca = ca
        self.insecureSkipVerify = insecureSkipVerify
        self.tenant = tenant
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            mode: try c.field(.mode, PromSource.proxy), kind: try c.field(.kind, ""), namespace: try c.field(.namespace, ""),
            service: try c.field(.service, ""), port: try c.field(.port, 0), pathPrefix: try c.field(.pathPrefix, ""),
            url: try c.field(.url, ""), auth: try c.field(.auth, ""), username: try c.field(.username, ""),
            secret: try c.field(.secret, ""), ca: try c.field(.ca, ""), insecureSkipVerify: try c.field(.insecureSkipVerify, false),
            tenant: try c.field(.tenant, ""))
    }

    private enum CodingKeys: String, CodingKey {
        case mode, kind, namespace, service, port, pathPrefix, url, auth, username, secret, ca, insecureSkipVerify, tenant
    }

    /// "monitoring/prometheus-operated:9090" or the URL.
    public var label: String { mode == PromSource.proxy ? "\(namespace)/\(service):\(port)\(pathPrefix)" : url }

    /// Never prints the secret (logs, crash reports).
    public var description: String { "PromSource(\(mode), \(label), auth=\(auth))" }

    /// This source in `mode`, without the other mode's fields (Go refuses credentials for the proxy).
    public func switched(to mode: String) -> PromSource {
        var s = self
        s.mode = mode
        if mode == PromSource.proxy {
            (s.url, s.auth, s.username, s.secret, s.ca, s.insecureSkipVerify) = ("", PromSource.authNone, "", "", "", false)
        } else {
            (s.namespace, s.service, s.port, s.pathPrefix, s.kind) = ("", "", 0, "", "")
        }
        return s
    }

    /// The same query API (discovery list selection).
    public func sameEndpoint(as other: PromSource) -> Bool {
        namespace == other.namespace && service == other.service && port == other.port && pathPrefix == other.pathPrefix
    }
}

public struct PromDiscovery: Decodable, Equatable, Sendable {
    public let sources: [PromSource]

    public init(sources: [PromSource] = []) { self.sources = sources }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        sources = try c.field(.sources, [])
    }

    private enum CodingKeys: String, CodingKey { case sources }
}

/// A query result: `times` in unix ms, each series' values aligned on them, nil for a gap.
public struct PromResult: Decodable, Equatable, Sendable {
    public let resultType: String
    public let times: [Int64]
    public let series: [PromSeries]
    public let warnings: [String]
    public let truncated: Bool
    public let total: Int

    public init(resultType: String = "", times: [Int64] = [], series: [PromSeries] = [], warnings: [String] = [], truncated: Bool = false, total: Int = 0) {
        self.resultType = resultType
        self.times = times
        self.series = series
        self.warnings = warnings
        self.truncated = truncated
        self.total = total
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        self.init(resultType: try c.field(.resultType, ""), times: try c.field(.times, []), series: try c.field(.series, []),
                  warnings: try c.field(.warnings, []), truncated: try c.field(.truncated, false), total: try c.field(.total, 0))
    }

    private enum CodingKeys: String, CodingKey { case resultType, times, series, warnings, truncated, total }
}

public struct PromSeries: Decodable, Equatable, Sendable {
    public let name: String
    public let labels: [String: String]
    public let values: [Double?]

    public init(name: String = "", labels: [String: String] = [:], values: [Double?] = []) {
        self.name = name
        self.labels = labels
        self.values = values
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        self.init(name: try c.field(.name, ""), labels: try c.field(.labels, [:]), values: try c.field(.values, []))
    }

    private enum CodingKeys: String, CodingKey { case name, labels, values }

    /// The name in a legend: `template` with {{label}} replaced, else the full series name.
    public func legend(_ template: String) -> String {
        let trimmed = template.trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty else { return name }
        var out = ""
        var rest = Substring(trimmed)
        while let open = rest.range(of: "{{") {
            out += rest[..<open.lowerBound]
            guard let close = rest[open.upperBound...].range(of: "}}") else {
                rest = rest[open.lowerBound...]
                break
            }
            let key = rest[open.upperBound..<close.lowerBound].trimmingCharacters(in: .whitespaces)
            out += labels[key] ?? ""
            rest = rest[close.upperBound...]
        }
        out += rest
        let result = out.trimmingCharacters(in: .whitespaces)
        return result.isEmpty ? name : result
    }

    /// The last value that is not a gap.
    public var latest: Double? { values.last { $0 != nil } ?? nil }
}

/// A saved chart. `unit`: percent, bytes, cores, persec, count or "" (plain). `legend` names
/// each series from its labels ("{{namespace}}/{{pod}}"), "" for the series name.
public struct PromPanel: Codable, Equatable, Hashable, Identifiable, Sendable {
    public var id: String
    public var title: String
    public var query: String
    public var unit: String
    public var legend: String

    public init(id: String = "", title: String = "", query: String = "", unit: String = "", legend: String = "") {
        self.id = id
        self.title = title
        self.query = query
        self.unit = unit
        self.legend = legend
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        self.init(id: try c.field(.id, ""), title: try c.field(.title, ""), query: try c.field(.query, ""),
                  unit: try c.field(.unit, ""), legend: try c.field(.legend, ""))
    }

    private enum CodingKeys: String, CodingKey { case id, title, query, unit, legend }

    public static let units = ["", "percent", "bytes", "cores", "persec", "count"]
}

/// A cluster's metrics setup; `source` nil until one is chosen.
public struct MetricsConfig: Codable, Equatable, Sendable {
    public var source: PromSource?
    public var panels: [PromPanel]

    public init(source: PromSource? = nil, panels: [PromPanel] = []) {
        self.source = source
        self.panels = panels
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        self.init(source: try c.decodeIfPresent(PromSource.self, forKey: .source), panels: try c.field(.panels, []))
    }

    private enum CodingKeys: String, CodingKey { case source, panels }

    /// Adds `panel`, or replaces the one with its id.
    public func saving(_ panel: PromPanel) -> MetricsConfig {
        var copy = self
        if let i = copy.panels.firstIndex(where: { $0.id == panel.id }) {
            copy.panels[i] = panel
        } else {
            copy.panels.append(panel)
        }
        return copy
    }

    /// The panel `id` moved by `delta` places (−1 up, +1 down); unchanged at the ends.
    public func moving(_ id: String, by delta: Int) -> MetricsConfig {
        guard let from = panels.firstIndex(where: { $0.id == id }), panels.indices.contains(from + delta) else { return self }
        var copy = self
        copy.panels.swapAt(from, from + delta)
        return copy
    }
}

/// The time ranges offered above the panels.
public enum MetricsRange: Int, CaseIterable, Sendable {
    case minutes15 = 900
    case hour1 = 3600
    case hours6 = 21600
    case day1 = 86400
    case days7 = 604800

    public var seconds: Int64 { Int64(rawValue) }
}

/// `value` in `unit`: "42.1%", "1.5 GiB", "0.25", "3.2/s", "7", or a compact plain number.
public func formatMetric(_ value: Double, unit: String) -> String {
    switch unit {
    case "percent": return String(format: "%.1f%%", value)
    case "bytes": return value < 0 ? "-" + metricBytes(-value) : metricBytes(value)
    case "cores": return String(format: "%.2f", value)
    case "persec": return compactMetric(value) + "/s"
    case "count": return abs(value) < 1e6 ? String(format: "%.0f", value) : compactMetric(value)
    default: return compactMetric(value)
    }
}

/// 1234567 -> "1.23M", 12.5 -> "12.5", 0.000123 -> "1.23e-04".
public func compactMetric(_ value: Double) -> String {
    let a = abs(value)
    switch a {
    case 0: return "0"
    case 1e12...: return String(format: "%.3g", value)
    case 1e9...: return String(format: "%.2fG", value / 1e9)
    case 1e6...: return String(format: "%.2fM", value / 1e6)
    case 1e4...: return String(format: "%.1fk", value / 1e3)
    case 100...: return String(format: "%.0f", value)
    case 0.01...:
        let s = String(format: "%.3g", value)
        guard s.contains(".") else { return s }
        var t = Substring(s)
        while t.hasSuffix("0") { t = t.dropLast() }
        if t.hasSuffix(".") { t = t.dropLast() }
        return String(t)
    default: return String(format: "%.2e", value)
    }
}

/// Binary units like the Android app: 1536 -> "1.5 KiB".
private func metricBytes(_ bytes: Double) -> String {
    let units = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"]
    guard bytes >= 1024 else { return String(format: "%.0f B", bytes) }
    var value = bytes
    var unit = 0
    while value >= 1024 && unit < units.count - 1 {
        value /= 1024
        unit += 1
    }
    return String(format: "%.1f", value) + " " + units[unit]
}
