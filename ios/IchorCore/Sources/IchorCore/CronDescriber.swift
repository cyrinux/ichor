import Foundation

/// A CronJob schedule in words, for VoiceOver: the common shapes only. The app words it; this
/// only recognises the shape, so it stays testable on Linux.
public enum CronDescription: Equatable, Sendable {
    /// Every day at hour:minute.
    case daily(hour: Int, minute: Int)
    /// Every week; weekday 1 is Sunday … 7 Saturday (Calendar's numbering).
    case weekly(weekday: Int, hour: Int, minute: Int)
    /// Every month on that day.
    case monthly(day: Int, hour: Int, minute: Int)
    /// Any other schedule, read as written.
    case raw(String)

    /// "09:05": 24-hour, as in the schedule.
    public static func clock(hour: Int, minute: Int) -> String {
        String(format: "%02d:%02d", hour, minute)
    }
}

public enum CronDescriber {
    private static let macros: [String: String] = [
        "@daily": "0 0 * * *", "@midnight": "0 0 * * *", "@weekly": "0 0 * * 0", "@monthly": "0 0 1 * *",
    ]
    private static let dayNames = ["sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6]

    /// "0 9 * * *" → daily at 09:00, "30 2 * * 1" → weekly on Monday, "0 9 1 * *" → monthly on
    /// the 1st; ranges, lists, steps and time zones stay raw.
    public static func describe(_ schedule: String) -> CronDescription {
        let trimmed = schedule.trimmingCharacters(in: .whitespacesAndNewlines)
        let expanded = macros[trimmed.lowercased()] ?? trimmed
        let fields = expanded.split(whereSeparator: \.isWhitespace).map(String.init)
        guard fields.count == 5,
            let minute = number(fields[0], in: 0...59),
            let hour = number(fields[1], in: 0...23),
            fields[3] == "*"
        else { return .raw(trimmed) }
        switch (fields[2], fields[4]) {
        case ("*", "*"):
            return .daily(hour: hour, minute: minute)
        case ("*", let dow):
            guard let day = weekday(dow) else { return .raw(trimmed) }
            return .weekly(weekday: day, hour: hour, minute: minute)
        case (let dom, "*"):
            guard let day = number(dom, in: 1...31) else { return .raw(trimmed) }
            return .monthly(day: day, hour: hour, minute: minute)
        default:
            return .raw(trimmed)
        }
    }

    private static func number(_ field: String, in range: ClosedRange<Int>) -> Int? {
        guard !field.isEmpty, field.allSatisfy({ $0.isASCII && $0.isNumber }), let n = Int(field), range.contains(n)
        else { return nil }
        return n
    }

    /// Cron's 0 or 7 (Sunday) … 6, or a three-letter name, as Calendar's 1 (Sunday) … 7.
    private static func weekday(_ field: String) -> Int? {
        if let n = number(field, in: 0...7) { return n % 7 + 1 }
        return dayNames[field.lowercased()].map { $0 + 1 }
    }
}
