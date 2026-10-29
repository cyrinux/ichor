import Foundation

// Background alerts on node temperatures (opt-in, Talos clusters only, see Monitor.swift): the
// Go core's ClusterSensorHealth, when a node runs hot or is throttled, and the English wording
// (BackgroundMonitor rebuilds it in the user's language). Same rules as Android's SensorIssues.kt.

/// Every node's hottest sensor and throttling (the Go core's ClusterSensorHealth).
public struct ClusterSensorHealth: Decodable, Equatable, Sendable {
    public let context: String
    public let nodes: [SensorNode]

    public init(context: String = "", nodes: [SensorNode] = []) {
        self.context = context
        self.nodes = nodes
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        context = try c.field(.context, "")
        nodes = try c.field(.nodes, [])
    }

    private enum CodingKeys: String, CodingKey { case context, nodes }
}

public struct SensorNode: Decodable, Equatable, Sendable {
    /// Its address in the talosconfig: the first part of every key.
    public let node: String
    public let hostname: String
    /// The warmest sensor; nil on a node without any (a VM).
    public let hottest: TemperatureSensor?
    public let throttled: Bool
    /// The node could not be read.
    public let error: String?

    public init(
        node: String, hostname: String, hottest: TemperatureSensor? = nil, throttled: Bool = false, error: String? = nil
    ) {
        self.node = node
        self.hostname = hostname
        self.hottest = hottest
        self.throttled = throttled
        self.error = error
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.field(.node, "")
        hostname = try c.field(.hostname, "")
        hottest = try c.decodeIfPresent(TemperatureSensor.self, forKey: .hottest)
        throttled = try c.field(.throttled, false)
        error = try c.decodeIfPresent(String.self, forKey: .error)
    }

    private enum CodingKeys: String, CodingKey { case node, hostname, hottest, throttled, error }
}

/// The temperature threshold: 0 is automatic (the sensor's crit − sensorMargin, else its max), else °C.
public let sensorThresholdAuto = 0
public let sensorThresholdRange = 60...100
/// The threshold a fixed temperature starts at.
public let sensorThresholdDefault = 85
/// How far under a sensor's crit the automatic threshold is, and how far under it a hot node must cool to clear.
public let sensorMargin = 5.0
public let sensorHysteresis = 3.0

/// A sensor issue's stored value, "severity|hot|hostname|label|celsius|limit" (running hot) or
/// "severity|throttled|hostname|label|celsius|0.0" (throttled, with its hottest sensor; "||0.0" on a
/// node without any, as before it was kept), split up.
public struct SensorIssue: Equatable, Sendable {
    public static let hot = "hot"
    public static let throttledKind = "throttled"

    /// dataCritical (running hot: alerts at once) or dataWarning (throttled: on its second check).
    public let severity: String
    public let kind: String
    public let hostname: String
    public let label: String
    public let celsius: Double
    public let limit: Double

    public init(
        severity: String, kind: String, hostname: String, label: String = "", celsius: Double = 0, limit: Double = 0
    ) {
        self.severity = severity
        self.kind = kind
        self.hostname = hostname
        self.label = label
        self.celsius = celsius
        self.limit = limit
    }

    public init(value: String) {
        let parts = value.split(separator: "|", maxSplits: 5, omittingEmptySubsequences: false).map(String.init)
        func part(_ i: Int) -> String { parts.count > i ? parts[i] : "" }
        severity = part(0).isEmpty ? dataWarning : part(0)
        kind = part(1).isEmpty ? Self.hot : part(1)
        hostname = part(2)
        label = part(3)
        celsius = Double(part(4)) ?? 0
        limit = Double(part(5)) ?? 0
    }

    public var isThrottled: Bool { kind == Self.throttledKind }

    /// The stored form; the text fields lose any "|" so the parts still split.
    public var value: String {
        let safe = { (s: String) in s.replacingOccurrences(of: "|", with: "/") }
        return [severity, kind, safe(hostname), safe(label), celsiusText(celsius), celsiusText(limit)].joined(
            separator: "|")
    }
}

/// "88.0": a temperature as the alerts write it.
public func celsiusText(_ value: Double) -> String {
    String(format: "%.1f", locale: Locale(identifier: "en_US_POSIX"), value)
}

/// The temperature `sensor` alerts at: `threshold` when set, else crit − margin, else max; nil without either.
public func sensorLimit(_ sensor: TemperatureSensor, threshold: Int) -> Double? {
    if threshold != sensorThresholdAuto {
        return Double(min(max(threshold, sensorThresholdRange.lowerBound), sensorThresholdRange.upperBound))
    }
    if sensor.critCelsius > 0 { return sensor.critCelsius - sensorMargin }
    if sensor.maxCelsius > 0 { return sensor.maxCelsius }
    return nil
}

/// The issues worth a notification, keyed "<node>|hot" and "<node>|throttled": a node whose
/// hottest sensor reaches its limit runs hot, and stays so until it is sensorHysteresis °C under
/// it (`known` says it was); a throttled node is a warning (alerted on its second check in a row).
/// A node that did not answer keeps its `known` issues; a node without sensors has none.
public func sensorIssuesOf(_ health: ClusterSensorHealth, threshold: Int, known: [String: String] = [:]) -> [String:
    String]
{
    var out: [String: String] = [:]
    for node in health.nodes {
        if let error = node.error, !error.isEmpty {
            let prefix = "\(node.node)|"
            for (key, value) in known where key.hasPrefix(prefix) { out[key] = value }
            continue
        }
        let hostname = node.hostname.isEmpty ? node.node : node.hostname
        if let sensor = node.hottest, let limit = sensorLimit(sensor, threshold: threshold) {
            let key = "\(node.node)|\(SensorIssue.hot)"
            let wasHot = known[key] != nil
            if sensor.celsius >= limit || (wasHot && sensor.celsius > limit - sensorHysteresis) {
                out[key] =
                    SensorIssue(
                        severity: dataCritical, kind: SensorIssue.hot, hostname: hostname, label: sensor.label,
                        celsius: sensor.celsius, limit: limit
                    ).value
            }
        }
        if node.throttled {
            out["\(node.node)|\(SensorIssue.throttledKind)"] =
                SensorIssue(
                    severity: dataWarning, kind: SensorIssue.throttledKind, hostname: hostname,
                    label: node.hottest?.label ?? "", celsius: node.hottest?.celsius ?? 0
                ).value
        }
    }
    return out
}

/// The sensor issues kept by the previous snapshot, when it is the same cluster and read them.
public func knownSensorIssues(_ previous: ClusterSnapshot?, context: String) -> [String: String] {
    guard let previous, previous.context == context, previous.sensorWatched, previous.sensorChecked else { return [:] }
    return previous.sensorIssues
}

/// English title of a sensor issue, or of its end.
func sensorTitle(_ issue: SensorIssue, problem: Bool) -> String {
    switch (issue.isThrottled, problem) {
    case (true, true) where issue.celsius > 0: "\(issue.hostname) is throttled (\(celsiusText(issue.celsius)) °C)"
    case (true, true): "\(issue.hostname) is throttled"
    case (true, false): "\(issue.hostname) no longer throttled"
    case (false, true): "\(issue.hostname) runs hot (\(celsiusText(issue.celsius)) °C)"
    case (false, false): "\(issue.hostname) back to a normal temperature"
    }
}

/// English text of a sensor issue.
func sensorText(_ issue: SensorIssue) -> String {
    issue.isThrottled
        ? "Its cores run well below their speed: check its cooling."
        : "\(issue.label), alert at \(celsiusText(issue.limit)) °C"
}

/// Node temperature issues (see evaluateTrack), keyed "sensor:<key>". A resolved one is named
/// from what the previous snapshot kept of it.
func evaluateSensors(previous: ClusterSnapshot?, current: ClusterSnapshot, comparable: Bool, alerts: inout [Alert])
    -> IssueTrack
{
    let known = previous?.sensorIssues ?? [:]
    return evaluateTrack(
        previous: previous?.sensorTrack, current: current.sensorTrack, comparable: comparable,
        severity: { SensorIssue(value: $0).severity },
        problem: { key, value in
            let issue = SensorIssue(value: value)
            return Alert(
                key: "sensor:\(key)", title: sensorTitle(issue, problem: true), text: sensorText(issue), problem: true)
        },
        cleared: { key in
            let issue = SensorIssue(value: known[key] ?? "")
            return Alert(key: "sensor:\(key)", title: sensorTitle(issue, problem: false), text: "", problem: false)
        },
        alerts: &alerts)
}
