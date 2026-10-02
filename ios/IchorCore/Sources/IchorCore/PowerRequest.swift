import Foundation

public enum PowerAction: String, CaseIterable, Identifiable, Sendable {
    case reboot, shutdown

    public var id: String { rawValue }
    public var title: String { self == .reboot ? "Reboot" : "Shut down" }
    public var verb: String { self == .reboot ? "reboot" : "shut down" }
}

/// `talosctl reboot -m`; descriptions follow the Talos v1.14 reboot sequence.
public enum RebootMode: String, CaseIterable, Identifiable, Sendable {
    case `default`, powercycle, force

    public var id: String { rawValue }
    public var cli: String { rawValue }

    public var label: String {
        switch self {
        case .default: "Graceful"
        case .powercycle: "Power cycle"
        case .force: "Force"
        }
    }

    public var details: String {
        switch self {
        case .default: "Stop pods and services, then reboot (kexec fast reboot when available)."
        case .powercycle: "Graceful stop, then a full firmware reboot instead of kexec."
        case .force: "Reboot immediately: pods and services are NOT stopped. Only for a stuck node."
        }
    }
}

public struct PowerRequest: Equatable, Sendable {
    public var action: PowerAction
    public var rebootMode: RebootMode
    /// `talosctl shutdown --force`: skip the Kubernetes cordon/drain.
    public var forceShutdown: Bool

    public init(action: PowerAction, rebootMode: RebootMode = .default, forceShutdown: Bool = false) {
        self.action = action
        self.rebootMode = rebootMode
        self.forceShutdown = forceShutdown
    }

    public var forced: Bool {
        switch action {
        case .reboot: rebootMode == .force
        case .shutdown: forceShutdown
        }
    }

    /// Button / auth-prompt label, e.g. "Force reboot", "Power cycle", "Shut down".
    public var title: String {
        if action == .reboot && rebootMode == .powercycle { return "Power cycle" }
        if forced { return "Force \(action.title.lowercased())" }
        return action.title
    }
}
