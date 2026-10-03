import Foundation
import IchorCore

// IchorCore keeps English strings (it is also tested on Linux, where
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
        case .machineConfig: String(localized: "Machine config")
        case .etcdSnapshot: String(localized: "etcd snapshot")
        case .serviceControl: String(localized: "Service control")
        case .issueConfig: String(localized: "Issue talosconfig")
        case .packetCapture: String(localized: "Packet capture")
        case .upgrade: String(localized: "Talos upgrade")
        case .etcdMemberActions: String(localized: "etcd member actions")
        case .resourceBrowser: String(localized: "Resources browser")
        case .supportBundle: String(localized: "Support bundle")
        case .workloads: String(localized: "Kubernetes workloads")
        case .cgroups: String(localized: "Cgroups and pressure")
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

extension ServiceAction {
    var localizedTitle: String {
        switch self {
        case .start: String(localized: "Start")
        case .stop: String(localized: "Stop")
        case .restart: String(localized: "Restart")
        }
    }

    var localizedDetails: String {
        switch self {
        case .start: String(localized: "Starts the service on this node.")
        case .stop: String(localized: "Stops the service on this node until it is started again or the node reboots.")
        case .restart: String(localized: "Stops and starts the service on this node.")
        }
    }

    /// "Restart kubelet on cp-1?"
    func confirmationTitle(service: String, hostname: String) -> String {
        switch self {
        case .start: String(localized: "Start \(service) on \(hostname)?")
        case .stop: String(localized: "Stop \(service) on \(hostname)?")
        case .restart: String(localized: "Restart \(service) on \(hostname)?")
        }
    }

    /// "cp-1: kubelet restart requested"
    func requestedMessage(service: String, hostname: String) -> String {
        switch self {
        case .start: String(localized: "\(hostname): \(service) start requested")
        case .stop: String(localized: "\(hostname): \(service) stop requested")
        case .restart: String(localized: "\(hostname): \(service) restart requested")
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

extension ConfigValidity {
    var localizedLabel: String {
        switch self {
        case .days30: String(localized: "30 days")
        case .days90: String(localized: "90 days")
        case .year: String(localized: "1 year")
        }
    }
}

extension ConnectionFilter {
    var localizedLabel: String {
        switch self {
        case .listening: String(localized: "Listening")
        case .all: String(localized: "All")
        }
    }
}

extension ChangelogSection {
    /// The heading of a known kind in the user's language; an unknown kind keeps its JSON title.
    var localizedTitle: String {
        switch knownKind {
        case .new?: String(localized: "New")
        case .fixed?: String(localized: "Fixed")
        case .faster?: String(localized: "Faster")
        case .breaking?: String(localized: "Breaking changes")
        case nil: englishTitle
        }
    }
}

extension ImageSort {
    var localizedLabel: String {
        switch self {
        case .name: String(localized: "Name")
        case .size: String(localized: "Size")
        case .created: String(localized: "Created")
        }
    }
}
