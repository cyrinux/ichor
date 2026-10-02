import Foundation

/// Severity parsed by the Go core from a log line ("" when it has none).
public enum LogLevel: String, CaseIterable, Sendable {
    case error, warn, info, debug, none = ""
}

public struct LogField: Decodable, Equatable, Hashable, Sendable {
    public let k: String
    public let v: String

    public init(k: String, v: String) {
        self.k = k
        self.v = v
    }

    private enum CodingKeys: String, CodingKey { case k, v }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        k = try c.decodeIfPresent(String.self, forKey: .k) ?? ""
        v = try c.decodeIfPresent(String.self, forKey: .v) ?? ""
    }

    /// error / err / reason values are shown in red.
    public var isErrorKey: Bool { ["error", "err", "reason"].contains(k.lowercased()) }
}

/// One structured log line from the Go core (NodeLogs/dmesg `entries`, ParseLogLine).
public struct LogEntry: Decodable, Equatable, Sendable {
    /// Unix milliseconds, 0 when unknown.
    public let ts: Int64
    /// error | warn | info | debug | ""
    public let level: String
    public let source: String
    public let msg: String
    public let fields: [LogField]
    public let raw: String

    public var logLevel: LogLevel { LogLevel(rawValue: level) ?? .none }

    public init(ts: Int64 = 0, level: String = "", source: String = "", msg: String,
                fields: [LogField] = [], raw: String) {
        self.ts = ts
        self.level = level
        self.source = source
        self.msg = msg
        self.fields = fields
        self.raw = raw
    }

    /// An unparsed line: no level, the whole line as message.
    public init(raw: String) {
        self.init(msg: raw, raw: raw)
    }

    private enum CodingKeys: String, CodingKey { case ts, level, source, msg, fields, raw }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        ts = try c.decodeIfPresent(Int64.self, forKey: .ts) ?? 0
        level = try c.decodeIfPresent(String.self, forKey: .level) ?? ""
        source = try c.decodeIfPresent(String.self, forKey: .source) ?? ""
        raw = try c.decodeIfPresent(String.self, forKey: .raw) ?? ""
        msg = try c.decodeIfPresent(String.self, forKey: .msg) ?? raw
        fields = try c.decodeIfPresent([LogField].self, forKey: .fields) ?? []
    }

    /// Same level, source, message and fields (time aside).
    public func sameContent(as other: LogEntry) -> Bool {
        level == other.level && source == other.source && msg == other.msg && fields == other.fields
    }
}

/// Entry JSON from ParseLogLine for a followed `line`; an unparsed entry when it does not decode.
public func parseLogEntry(json: String, line: String) -> LogEntry {
    guard let entry = try? TalosJSON.decode(LogEntry.self, from: json) else { return LogEntry(raw: line) }
    if entry.raw.isEmpty {
        return LogEntry(ts: entry.ts, level: entry.level, source: entry.source, msg: entry.msg,
                        fields: entry.fields, raw: line)
    }
    return entry
}

public extension LogTail {
    /// The parsed entries, or unparsed ones built from `lines` when the core sent none (older
    /// core) or they do not line up with `lines`.
    var logEntries: [LogEntry] {
        if let entries, entries.count == lines.count { return entries }
        return lines.map(LogEntry.init(raw:))
    }
}

/// A log entry with a stable id (positions shift once old lines are dropped).
public struct LogItem: Equatable, Identifiable, Sendable {
    public let id: Int
    public let entry: LogEntry

    public init(id: Int, entry: LogEntry) {
        self.id = id
        self.entry = entry
    }
}

/// Level picker: everything, warnings and errors, or errors only.
public enum LogLevelFilter: String, CaseIterable, Sendable {
    case all, warnings, errors

    public func matches(_ level: LogLevel) -> Bool {
        switch self {
        case .all: true
        case .warnings: level == .warn || level == .error
        case .errors: level == .error
        }
    }
}

public struct LogLevelCounts: Equatable, Sendable {
    public let all: Int
    public let warnings: Int
    public let errors: Int

    public init(all: Int, warnings: Int, errors: Int) {
        self.all = all
        self.warnings = warnings
        self.errors = errors
    }

    public func count(_ filter: LogLevelFilter) -> Int {
        switch filter {
        case .all: all
        case .warnings: warnings
        case .errors: errors
        }
    }
}

/// Lines per level filter (each line counted, before collapsing).
public func logLevelCounts(_ items: [LogItem]) -> LogLevelCounts {
    var warnings = 0
    var errors = 0
    for item in items {
        switch item.entry.logLevel {
        case .error:
            errors += 1
            warnings += 1
        case .warn: warnings += 1
        default: break
        }
    }
    return LogLevelCounts(all: items.count, warnings: warnings, errors: errors)
}

/// Items whose raw line contains `search` (case-insensitive); all of them when it is empty.
public func searchLogs(_ items: [LogItem], _ search: String) -> [LogItem] {
    search.isEmpty ? items : items.filter { $0.entry.raw.localizedCaseInsensitiveContains(search) }
}

/// A run of identical consecutive entries shown as one row: the newest one, how many, and the
/// time range (0 when unknown). Its id is the first item's, so it is stable while the run grows
/// (the first item's styled text matches the newest one's, see LogEntry.sameContent).
public struct LogGroup: Equatable, Identifiable, Sendable {
    public let id: Int
    public let entry: LogEntry
    public let count: Int
    public let firstTs: Int64
    public let lastTs: Int64

    public init(id: Int, entry: LogEntry, count: Int, firstTs: Int64, lastTs: Int64) {
        self.id = id
        self.entry = entry
        self.count = count
        self.firstTs = firstTs
        self.lastTs = lastTs
    }
}

/// Collapses consecutive identical entries (oldest first in and out).
public func collapseLogs(_ items: [LogItem]) -> [LogGroup] {
    var groups: [LogGroup] = []
    for item in items {
        if let last = groups.last, last.entry.sameContent(as: item.entry) {
            let firstTs = last.firstTs != 0 ? last.firstTs : item.entry.ts
            let lastTs = item.entry.ts != 0 ? item.entry.ts : last.lastTs
            groups[groups.count - 1] = LogGroup(id: last.id, entry: item.entry, count: last.count + 1,
                                                firstTs: firstTs, lastTs: lastTs)
        } else {
            groups.append(LogGroup(id: item.id, entry: item.entry, count: 1, firstTs: item.entry.ts, lastTs: item.entry.ts))
        }
    }
    return groups
}

/// A row of the log list: a date divider (start of that local day) or an entry group.
/// Dividers get negative ids derived from the group below them, so ids stay unique and stable.
public enum LogRow: Equatable, Identifiable, Sendable {
    case day(Date, id: Int)
    case group(LogGroup)

    public var id: Int {
        switch self {
        case .day(_, let id): id
        case .group(let group): group.id
        }
    }
}

/// Groups with a date divider above the first dated one and wherever the local day changes
/// (undated entries never start a new day).
public func logRows(_ groups: [LogGroup], calendar: Calendar = .current) -> [LogRow] {
    var rows: [LogRow] = []
    rows.reserveCapacity(groups.count + 1)
    var currentDay: Date?
    for group in groups {
        if group.firstTs != 0 {
            let day = calendar.startOfDay(for: logDate(group.firstTs))
            if day != currentDay {
                rows.append(.day(day, id: -(group.id + 1)))
                currentDay = day
            }
        }
        rows.append(.group(group))
    }
    return rows
}

/// Search, level filter, collapse, then date dividers.
public func logRows(_ items: [LogItem], search: String, level: LogLevelFilter,
                    calendar: Calendar = .current) -> [LogRow] {
    let shown = searchLogs(items, search).filter { level.matches($0.entry.logLevel) }
    return logRows(collapseLogs(shown), calendar: calendar)
}

public func logDate(_ millis: Int64) -> Date {
    Date(timeIntervalSince1970: TimeInterval(millis) / 1000)
}

/// Local time of day as HH:mm:ss.SSS.
public func logTimeOfDay(_ millis: Int64, calendar: Calendar = .current) -> String {
    let parts = calendar.dateComponents([.hour, .minute, .second], from: logDate(millis))
    let ms = Int(((millis % 1000) + 1000) % 1000)
    return String(format: "%02d:%02d:%02d.%03d", parts.hour ?? 0, parts.minute ?? 0, parts.second ?? 0, ms)
}
