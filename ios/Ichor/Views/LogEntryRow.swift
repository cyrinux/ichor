import SwiftUI
import IchorCore

/// Parsed log items with their styled text, built once per entry (not on every render).
struct LogDocument: Sendable {
    var items: [LogItem] = []
    var texts: [Int: AttributedString] = [:]
    var truncated = false

    init() {}

    /// Snapshot of a service or kernel log.
    init(_ tail: LogTail) {
        let items = tail.logEntries.enumerated().map { LogItem(id: $0.offset, entry: $0.element) }
        self.items = items
        texts = LogStyle.texts(items)
        truncated = tail.truncated
    }

    /// The document with `new` appended, keeping at most `cap` items (dropping the oldest).
    func appending(_ new: [LogItem], texts newTexts: [Int: AttributedString], cap: Int) -> LogDocument {
        var next = LogDocument()
        next.items = appendCapped(items, new, cap: cap)
        let oldest = next.items.first?.id ?? 0
        next.texts = texts.filter { $0.key >= oldest }.merging(newTexts) { _, new in new }
        next.truncated = truncated
        return next
    }
}

/// Colors and text of a log entry: level tag, source, message and key=value fields.
enum LogStyle {
    static let font = Font.system(size: 11, design: .monospaced)
    private static let boldFont = Font.system(size: 11, weight: .bold, design: .monospaced)

    static func color(_ level: LogLevel) -> Color? {
        switch level {
        case .error: .red
        case .warn: .orange
        case .info: .secondary
        case .debug: Color.secondary.opacity(0.6)
        case .none: nil
        }
    }

    static func tag(_ level: LogLevel) -> String? {
        switch level {
        case .error: "ERR"
        case .warn: "WRN"
        case .info: "INF"
        case .debug: "DBG"
        case .none: nil
        }
    }

    static func texts(_ items: [LogItem]) -> [Int: AttributedString] {
        Dictionary(uniqueKeysWithValues: items.map { ($0.id, text($0.entry)) })
    }

    static func text(_ entry: LogEntry) -> AttributedString {
        let level = entry.logLevel
        var result = AttributedString()
        if let tag = tag(level), let color = color(level) {
            var run = AttributedString(tag + " ")
            run.foregroundColor = color
            run.font = boldFont
            result.append(run)
        }
        if !entry.source.isEmpty {
            var run = AttributedString(entry.source)
            run.foregroundColor = Color.secondary
            run.backgroundColor = Color.secondary.opacity(0.12)
            result.append(run)
            result.append(AttributedString(" "))
        }
        var message = AttributedString(entry.msg)
        if level == .debug { message.foregroundColor = Color.secondary }
        result.append(message)
        for field in entry.fields {
            var key = AttributedString(" \(field.k)=")
            key.foregroundColor = field.isErrorKey ? Color.red : Color.secondary
            var value = AttributedString(field.v)
            value.foregroundColor = field.isErrorKey ? Color.red : Color.teal
            result.append(key)
            result.append(value)
        }
        return result
    }

    /// "08:00:00.123 " or "08:00:00.123–08:00:05.000 " (dimmed), empty when the time is unknown.
    static func timePrefix(_ group: LogGroup) -> AttributedString {
        guard group.firstTs != 0 else { return AttributedString() }
        var time = logTimeOfDay(group.firstTs)
        if group.count > 1 && group.lastTs != group.firstTs && group.lastTs != 0 {
            time += "–" + logTimeOfDay(group.lastTs)
        }
        var run = AttributedString(time + " ")
        run.foregroundColor = Color.secondary
        return run
    }
}

/// One log row: colored stripe, time, ×N and styled entry; a tap shows the newest raw line.
struct LogEntryRow: View {
    let group: LogGroup
    let text: AttributedString?
    let expanded: Bool
    let toggle: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(line)
            if expanded {
                Text(verbatim: group.entry.raw)
                    .foregroundStyle(.secondary)
                    .textSelection(.enabled)
            }
        }
        .font(LogStyle.font)
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.leading, 7)
        .background(alignment: .leading) {
            if let color = LogStyle.color(group.entry.logLevel) {
                color.frame(width: 3)
            }
        }
        .contentShape(Rectangle())
        .onTapGesture(perform: toggle)
        .accessibilityAddTraits(.isButton)
        .accessibilityValue(expanded ? Text("Expanded") : Text("Collapsed"))
        .accessibilityHint(group.count > 1 ? Text("Repeated \(group.count) times") : Text(verbatim: ""))
    }

    private var line: AttributedString {
        var result = LogStyle.timePrefix(group)
        if group.count > 1 {
            var count = AttributedString("×\(group.count) ")
            count.foregroundColor = Color.secondary
            count.font = Font.system(size: 11, weight: .semibold, design: .monospaced)
            result.append(count)
        }
        result.append(text ?? LogStyle.text(group.entry))
        return result
    }
}

/// Small header above the first line of each day.
struct LogDayHeader: View {
    let day: Date

    var body: some View {
        Text(verbatim: day.formatted(date: .complete, time: .omitted))
            .font(.caption2.weight(.semibold))
            .foregroundStyle(.secondary)
            .frame(maxWidth: .infinity, alignment: .center)
            .listRowSeparator(.hidden)
    }
}

/// An unparsed raw line ("Raw" mode).
struct LogRawLine: View {
    let text: String

    var body: some View {
        Text(verbatim: text)
            .font(LogStyle.font)
            .textSelection(.enabled)
    }
}

/// A row of the parsed list (date header or entry group).
struct LogRowView: View {
    let row: LogRow
    let texts: [Int: AttributedString]
    @Binding var expanded: Set<Int>

    var body: some View {
        switch row {
        case .day(let day, _):
            LogDayHeader(day: day)
        case .group(let group):
            LogEntryRow(group: group, text: texts[group.id], expanded: expanded.contains(group.id)) {
                if expanded.contains(group.id) { expanded.remove(group.id) } else { expanded.insert(group.id) }
            }
        }
    }
}

/// All / Warnings+ / Errors with line counts, above the list.
struct LogLevelPicker: View {
    @Binding var level: LogLevelFilter
    let counts: LogLevelCounts

    var body: some View {
        Picker("Level", selection: $level) {
            ForEach(LogLevelFilter.allCases, id: \.self) { filter in
                Text(label(filter)).tag(filter)
            }
        }
        .pickerStyle(.segmented)
        .padding(.horizontal)
        .padding(.vertical, 6)
        .background(.bar)
    }

    private func label(_ filter: LogLevelFilter) -> String {
        let count = counts.count(filter)
        switch filter {
        case .all: return String(localized: "All · \(count)")
        case .warnings: return String(localized: "Warnings+ · \(count)")
        case .errors: return String(localized: "Errors · \(count)")
        }
    }
}
