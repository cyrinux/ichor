import Foundation

// Mirrors go/ichorgo/kube_workload_logs.go: the logs of a workload's pods, merged by time.

/// The time ranges a log sheet offers, in minutes; 0 for no bound.
public let logSinceChoices = [15, 60, 360, 1440, 0]

/// The lines per pod the workload log sheet offers.
public let workloadLogTails = [200, 500, 2000]

/// How many colours the core hands out to a workload's pods.
public let workloadLogColors = 8

/// What a log sheet keeps: lines holding `text` (a Go regular expression when `regex`), from
/// the last `sinceMinutes` (0: no bound).
public struct LogFilter: Equatable, Hashable, Sendable {
    public var text: String
    public var regex: Bool
    public var sinceMinutes: Int

    public init(text: String = "", regex: Bool = false, sinceMinutes: Int = 0) {
        self.text = text
        self.regex = regex
        self.sinceMinutes = sinceMinutes
    }
}

public struct WorkloadLogPod: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    public let container: String
    /// 0 until `workloadLogColors`.
    public let color: Int
    public let lines: Int
    /// Its tail or its size cap was reached.
    public let truncated: Bool

    public var id: String { name }

    private enum CodingKeys: String, CodingKey { case name, container, color, lines, truncated }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        container = try c.field(.container, "")
        color = try c.field(.color, 0)
        lines = try c.field(.lines, 0)
        truncated = try c.field(.truncated, false)
    }
}

public struct WorkloadLogLine: Decodable, Equatable, Sendable {
    public let pod: String
    /// Unix ms, 0 when unknown.
    public let time: Int64
    public let text: String
    public let level: String

    public init(pod: String, time: Int64 = 0, text: String = "", level: String = "") {
        self.pod = pod
        self.time = time
        self.text = text
        self.level = level
    }

    private enum CodingKeys: String, CodingKey { case pod, time, text, level }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        pod = try c.field(.pod, "")
        time = try c.field(.time, 0)
        text = try c.field(.text, "")
        level = try c.field(.level, "")
    }

    /// What tells the pods of a workload apart on a chip: "x2k4" for "api-7d9f-x2k4".
    public var podSuffix: String { pod.split(separator: "-").last.map(String.init) ?? pod }
}

public struct WorkloadLogs: Decodable, Equatable, Sendable {
    public let pods: [WorkloadLogPod]
    public let lines: [WorkloadLogLine]
    public let matched: Int
    public let scanned: Int
    public let truncated: Bool
    public let sinceMinutes: Int

    private enum CodingKeys: String, CodingKey { case pods, lines, matched, scanned, truncated, sinceMinutes }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        pods = try c.field(.pods, [])
        lines = try c.field(.lines, [])
        matched = try c.field(.matched, 0)
        scanned = try c.field(.scanned, 0)
        truncated = try c.field(.truncated, false)
        sinceMinutes = try c.field(.sinceMinutes, 0)
    }

    /// The colour slot of `pod`, wrapped into the palette; 0 for one not listed.
    public func colorSlot(of pod: String) -> Int {
        let color = pods.first { $0.name == pod }?.color ?? 0
        return ((color % workloadLogColors) + workloadLogColors) % workloadLogColors
    }

    /// The merged log as text, to copy or share: one `time pod text` per line.
    public var asText: String {
        let format = ISO8601DateFormatter()
        return lines.map { line in
            let time = line.time > 0 ? format.string(from: Date(timeIntervalSince1970: Double(line.time) / 1000)) : "-"
            return "\(time) \(line.pod) \(line.text)\n"
        }.joined()
    }
}
