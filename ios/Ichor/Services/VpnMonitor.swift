import Foundation
import IchorCore
import Network
import Observation

/// Whether a VPN is up, followed with NWPathMonitor (see IchorCore.vpnIsUp: a tunnel interface
/// in the current path, or the default route through one). iOS has no public "VPN connected"
/// API; tunnels (utun, ipsec, ppp) only show up in the path while the VPN is connected.
@Observable
@MainActor
final class VpnMonitor {
    static let shared = VpnMonitor()

    private(set) var up = false
    /// Called each time a VPN comes up, so a VPN-only cluster on screen reloads.
    @ObservationIgnored var onConnect: (() -> Void)?
    @ObservationIgnored private let monitor = NWPathMonitor()

    private init() {
        monitor.pathUpdateHandler = { path in
            let isUp = Self.isUp(path)
            Task { @MainActor in VpnMonitor.shared.update(isUp) }
        }
        monitor.start(queue: DispatchQueue(label: "name.levis.ichor.vpn-monitor"))
    }

    private func update(_ isUp: Bool) {
        guard isUp != up else { return }
        up = isUp
        if isUp { onConnect?() }
    }

    nonisolated static func isUp(_ path: NWPath) -> Bool {
        vpnIsUp(interfaces: path.availableInterfaces.map { PathInterface(name: $0.name, isOther: $0.type == .other) },
                usesOther: path.usesInterfaceType(.other))
    }

    /// The current answer, read once from a fresh monitor: for background checks, which may
    /// run before the shared monitor reported.
    nonisolated static func currentlyUp() async -> Bool {
        await withCheckedContinuation { continuation in
            let monitor = NWPathMonitor()
            let once = OnceFlag()
            monitor.pathUpdateHandler = { path in
                guard once.claim() else { return }
                monitor.cancel()
                continuation.resume(returning: isUp(path))
            }
            monitor.start(queue: DispatchQueue(label: "name.levis.ichor.vpn-check"))
        }
    }
}

/// True for the first caller only (path updates come on one queue, but may repeat).
private final class OnceFlag: @unchecked Sendable {
    private let lock = NSLock()
    private var claimed = false

    func claim() -> Bool {
        lock.lock()
        defer { lock.unlock() }
        if claimed { return false }
        claimed = true
        return true
    }
}
