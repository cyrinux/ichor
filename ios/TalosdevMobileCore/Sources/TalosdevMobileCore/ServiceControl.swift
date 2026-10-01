import Foundation

/// `talosctl service SERVICE start|stop|restart` (os:operator or os:admin).
public enum ServiceAction: String, CaseIterable, Identifiable, Sendable {
    case start, stop, restart

    public var id: String { rawValue }
}

/// Services whose stop or restart can cut the node (or the cluster) off: asked with an extra warning.
public let criticalServices: Set<String> = ["apid", "trustd", "etcd", "kubelet", "machined", "containerd", "cri"]

public func isCriticalService(_ id: String) -> Bool { criticalServices.contains(id.lowercased()) }

/// Talos service state "Running" (case-insensitive).
public func isServiceRunning(state: String) -> Bool { state.caseInsensitiveCompare("Running") == .orderedSame }

/// Restart always; Stop when running, Start otherwise.
public func serviceActions(state: String) -> [ServiceAction] {
    isServiceRunning(state: state) ? [.restart, .stop] : [.restart, .start]
}

/// Lines with the new ones appended, keeping at most `cap` (dropping the oldest).
public func appendCapped<T>(_ lines: [T], _ new: [T], cap: Int) -> [T] {
    let all = lines + new
    return all.count > cap ? Array(all.suffix(cap)) : all
}
