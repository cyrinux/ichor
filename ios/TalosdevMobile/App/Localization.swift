import Foundation
import TalosdevMobileCore

// TalosdevMobileCore keeps English strings (it is also tested on Linux, where
// String(localized:) does not exist); the app localizes them here, with keys in
// Localizable.xcstrings.

extension Feature {
    var localizedLabel: String {
        switch self {
        case .power: String(localized: "Reboot / shutdown")
        case .health: String(localized: "Cluster health check")
        case .kubeconfig: String(localized: "Kubeconfig export")
        case .debugShell: String(localized: "Debug shell")
        case .etcdDefrag: String(localized: "etcd defragmentation")
        }
    }
}

extension PowerAction {
    var localizedTitle: String {
        switch self {
        case .reboot: String(localized: "Reboot")
        case .shutdown: String(localized: "Shut down")
        }
    }
}

extension RebootMode {
    var localizedLabel: String {
        switch self {
        case .default: String(localized: "Graceful")
        case .powercycle: String(localized: "Power cycle")
        case .force: String(localized: "Force")
        }
    }

    var localizedDetails: String {
        switch self {
        case .default: String(localized: "Stop pods and services, then reboot (kexec fast reboot when available).")
        case .powercycle: String(localized: "Graceful stop, then a full firmware reboot instead of kexec.")
        case .force: String(localized: "Reboot immediately: pods and services are NOT stopped. Only for a stuck node.")
        }
    }
}

extension PowerRequest {
    /// Same rules as `title`: "Force reboot", "Power cycle", "Shut down"…
    var localizedTitle: String {
        if action == .reboot && rebootMode == .powercycle { return String(localized: "Power cycle") }
        if forced {
            return action == .reboot ? String(localized: "Force reboot") : String(localized: "Force shutdown")
        }
        return action.localizedTitle
    }
}

extension ContextSummary {
    /// Localized `accessLabel`: "admin", "operator" or "read-only".
    var localizedAccessLabel: String {
        if roles.contains("os:admin") { return String(localized: "admin") }
        if roles.contains("os:operator") { return String(localized: "operator") }
        return String(localized: "read-only")
    }
}

/// Localized `certExpiryText`: "in 12 days" / "expired 3 days ago".
func localizedCertExpiry(_ notAfter: Int64, now: Date = Date()) -> String {
    let days = daysUntil(notAfter, now: now)
    return days < 0 ? String(localized: "expired \(-days) days ago") : String(localized: "in \(days) days")
}

/// Localized `formatDuration`: "3d 4h", "5h 12m", "42m" or "<1m", in the user's language.
func localizedDuration(_ seconds: Int64) -> String {
    if seconds < 60 { return String(localized: "<1 min") }
    let formatter = DateComponentsFormatter()
    formatter.unitsStyle = .abbreviated
    formatter.maximumUnitCount = 2
    formatter.allowedUnits = seconds >= 86_400 ? [.day, .hour] : seconds >= 3_600 ? [.hour, .minute] : [.minute]
    return formatter.string(from: TimeInterval(seconds)) ?? formatDuration(seconds)
}
