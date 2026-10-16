import Foundation

/// The windows the incident summary (CYR-77) is shared for: the last 6 hours, day or week.
/// The Go core takes the window as epoch ms (IncidentSummary's since and now).
public enum IncidentWindow: Int, CaseIterable, Identifiable, Sendable {
    case sixHours = 6
    case day = 24
    case week = 168

    /// The window the sheet opens on.
    public static let standard: IncidentWindow = .day

    public var id: Int { rawValue }

    /// Its length in hours.
    public var hours: Int { rawValue }

    /// The start of the window that ends at `now` (epoch ms), never before the epoch.
    public func since(now: Int64) -> Int64 { max(0, now - Int64(rawValue) * 3_600_000) }
}
