import Foundation
import IchorCore

/// How a volume's fill projection is worded (CYR-128), on the storage screen and in the trend
/// alert. Same as Android.
extension ForecastDays {
    /// After "full" or "critical": "in < 1 day", "in ~1 day", "in ~3 days".
    var localized: String {
        switch self {
        case .underADay: String(localized: "in < 1 day")
        case .aboutADay: String(localized: "in ~1 day")
        case .days(let days): String(localized: "in ~\(days) days")
        }
    }
}

extension VolumeForecastLine {
    /// "Full in ~22 days · critical in ~20 days", or "Growing ~0.5 % a day" when not projected.
    var localized: String {
        switch self {
        case .projected(let full, let critical):
            guard let full else {
                // Full beyond a year, critical within it.
                return critical.map { String(localized: "Critical \($0.localized)") } ?? ""
            }
            let fullText = String(localized: "Full \(full.localized)")
            guard let critical else { return fullText }
            let criticalText = String(localized: "critical \(critical.localized)")
            return "\(fullText) · \(criticalText)"
        case .growing(let growth):
            let rate = "\(growth) %"
            return String(localized: "Growing ~\(rate) a day")
        }
    }
}
