import Foundation

// Top messages: the Go core's LogTemplates (go/ichorgo/logtemplates.go) groups a loaded log into
// its repeated messages, the parts that change from line to line masked as <*>. Same as Android.

/// Fewer lines than this and grouping says nothing a glance at the log does not.
public let logTemplatesMinLines = 20

/// How many repeated messages the sheet lists.
public let logTemplatesLimit = 20

/// The placeholder Go puts in place of a masked token.
public let logTemplateHole = "<*>"

/// A log's most repeated messages, most lines first.
public struct LogTemplates: Decodable, Equatable, Sendable {
    public let groups: [LogTemplateGroup]
    public let lines: Int
    public let distinct: Int

    public init(groups: [LogTemplateGroup] = [], lines: Int = 0, distinct: Int = 0) {
        self.groups = groups
        self.lines = lines
        self.distinct = distinct
    }

    private enum CodingKeys: String, CodingKey { case groups, lines, distinct }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        groups = try c.field(.groups, [])
        lines = try c.field(.lines, 0)
        distinct = try c.field(.distinct, 0)
    }
}

/// One repeated message: `template` with its variable parts as <*>, `count` lines of `share` of
/// the log, `sample` the newest one as written (what the filter is set to), `pods` how many of a
/// workload's pods wrote it (0 for one pod's log).
public struct LogTemplateGroup: Decodable, Equatable, Identifiable, Sendable {
    public let template: String
    public let level: String
    public let source: String
    public let count: Int
    public let share: Double
    public let firstTs: Int64
    public let lastTs: Int64
    public let sample: String
    public let pods: Int

    public var id: String { "\(level)|\(source)|\(template)" }

    public init(
        template: String, level: String = "", source: String = "", count: Int = 0, share: Double = 0,
        firstTs: Int64 = 0, lastTs: Int64 = 0, sample: String = "", pods: Int = 0
    ) {
        self.template = template
        self.level = level
        self.source = source
        self.count = count
        self.share = share
        self.firstTs = firstTs
        self.lastTs = lastTs
        self.sample = sample
        self.pods = pods
    }

    private enum CodingKeys: String, CodingKey {
        case template, level, source, count, share, firstTs, lastTs, sample, pods
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        template = try c.field(.template, "")
        level = try c.field(.level, "")
        source = try c.field(.source, "")
        count = try c.field(.count, 0)
        share = try c.field(.share, 0)
        firstTs = try c.field(.firstTs, 0)
        lastTs = try c.field(.lastTs, 0)
        sample = try c.field(.sample, "")
        pods = try c.field(.pods, 0)
    }

    /// "61 %", rounded; "<1 %" for a message that is there but rare.
    public var shareText: String {
        let percent = Int((share * 100).rounded())
        return percent < 1 && count > 0 ? "<1 %" : "\(percent) %"
    }

    /// The template split around its placeholders, for a row that dims them: (text, isHole).
    public var parts: [(text: String, hole: Bool)] {
        let pieces = template.components(separatedBy: logTemplateHole)
        var out: [(text: String, hole: Bool)] = []
        for (i, piece) in pieces.enumerated() {
            if !piece.isEmpty { out.append((text: piece, hole: false)) }
            if i < pieces.count - 1 { out.append((text: logTemplateHole, hole: true)) }
        }
        return out
    }
}

/// What LogTemplates reads of a workload's merged lines: each one with its pod.
private struct LogTemplateLine: Encodable {
    let pod: String
    let time: Int64
    let text: String
    let level: String
}

/// A pod's log `lines` as LogTemplates takes them (a JSON array of strings).
public func logTemplatesInput(_ lines: [String]) -> String {
    (try? JSONEncoder().encode(lines)).flatMap { String(data: $0, encoding: .utf8) } ?? "[]"
}

/// A workload's merged `lines` as LogTemplates takes them (with their pods).
public func logTemplatesInput(_ lines: [WorkloadLogLine]) -> String {
    let rows = lines.map { LogTemplateLine(pod: $0.pod, time: $0.time, text: $0.text, level: $0.level) }
    return (try? JSONEncoder().encode(rows)).flatMap { String(data: $0, encoding: .utf8) } ?? "[]"
}
