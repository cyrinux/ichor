import Foundation

private let units = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"]

/// 1536 -> "1.5 KiB" (same output as the Android app).
public func formatBytes<T: BinaryInteger>(_ bytes: T) -> String {
    if bytes < 1024 { return "\(bytes) B" }
    var value = Double(bytes)
    var unit = 0
    while value >= 1024 && unit < units.count - 1 {
        value /= 1024
        unit += 1
    }
    return String(format: "%.1f %@", value, units[unit])
}

/// Seconds -> "3d 4h", "5h 12m", "42m" or "<1m".
public func formatDuration(_ seconds: Int64) -> String {
    if seconds < 60 { return "<1m" }
    let days = seconds / 86_400
    let hours = (seconds % 86_400) / 3_600
    let minutes = (seconds % 3_600) / 60
    if days > 0 { return "\(days)d \(hours)h" }
    if hours > 0 { return "\(hours)h \(minutes)m" }
    return "\(minutes)m"
}

public extension Optional where Wrapped == String {
    /// The string, or nil when it is nil or empty.
    var nonEmpty: String? { flatMap { $0.isEmpty ? nil : $0 } }
}

public extension Date {
    /// A Unix-milliseconds instant.
    init(epochMillis: Int64) {
        self.init(timeIntervalSince1970: TimeInterval(epochMillis) / 1000)
    }
}

/// `date` as `format` in `timeZone`, with the POSIX locale so file names never vary by region.
func fileTimestamp(_ date: Date, format: String, timeZone: TimeZone) -> String {
    let formatter = DateFormatter()
    formatter.locale = Locale(identifier: "en_US_POSIX")
    formatter.timeZone = timeZone
    formatter.dateFormat = format
    return formatter.string(from: date)
}

/// Fraction used in [0, 1]; 0 when total is unknown.
public func usedFraction(total: UInt64, available: UInt64) -> Double {
    guard total > 0 else { return 0 }
    let used = available >= total ? 0 : total - available
    return Double(used) / Double(total)
}

/// Days until an epoch-seconds deadline, negative when past (floored).
public func daysUntil(_ epochSeconds: Int64, now: Date = Date()) -> Int {
    let delta = Double(epochSeconds) - now.timeIntervalSince1970
    return Int((delta / 86_400).rounded(.down))
}

public func certExpiryText(_ notAfter: Int64, now: Date = Date()) -> String {
    let days = daysUntil(notAfter, now: now)
    return days < 0 ? "expired \(-days) days ago" : "in \(days) days"
}
