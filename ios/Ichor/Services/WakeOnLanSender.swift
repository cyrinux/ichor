import Foundation
import IchorCore
import Network

/// Sends Wake-on-LAN magic packets straight from the phone (no Talos API involved: the node is
/// off), over UDP with Network.framework. The phone has to reach the target address: the node's
/// LAN, or a router or relay that forwards the packet there.
///
/// Broadcast: without Apple's com.apple.developer.networking.multicast entitlement, iOS may
/// refuse to send to a broadcast address (255.255.255.255 or the subnet's .255). Sending to a
/// relay or another unicast address needs no entitlement; the error then says to use one.
enum WakeOnLanSender {
    /// UDP gives no delivery report: a few copies, a little apart, make a lost one harmless.
    private static let copies = 3
    private static let gap: Duration = .milliseconds(100)
    /// Seconds to wait for a path to send on.
    private static let timeout: Double = 5

    /// What was sent to: the given address, or the local network's broadcast one.
    static func send(_ target: WolTarget) async throws -> String {
        guard let mac = parseMac(target.mac), let port = NWEndpoint.Port(rawValue: UInt16(clamping: target.port)) else {
            throw TalosError(message: String(localized: "Not a MAC address (e.g. aa:bb:cc:dd:ee:ff)"))
        }
        // No address given means "the network the phone is on": its Wi-Fi or Ethernet one.
        let local = target.broadcast.isEmpty
        let destination = local ? (LocalNetwork.directedBroadcastAddress() ?? wolDefaultBroadcast) : target.broadcast
        let parameters = NWParameters.udp
        if local { parameters.prohibitedInterfaceTypes = [.cellular] }
        let connection = NWConnection(host: NWEndpoint.Host(destination), port: port, using: parameters)
        defer { connection.cancel() }
        do {
            try await ready(connection)
            let payload = Data(magicPacket(mac))
            for copy in 0..<copies {
                if copy > 0 { try await Task.sleep(for: gap) }
                try await send(payload, on: connection)
            }
        } catch {
            throw failure(error, broadcast: local || isBroadcast(destination))
        }
        return destination
    }

    /// A broadcast address, as far as the phone can tell: all-ones, or ending in .255.
    private static func isBroadcast(_ address: String) -> Bool {
        address == wolDefaultBroadcast || address.hasSuffix(".255")
    }

    private static func failure(_ error: Error, broadcast: Bool) -> Error {
        let reason = (error as? NWError).map { "\($0)" } ?? error.localizedDescription
        guard broadcast else { return TalosError(message: reason) }
        let hint = String(localized: "iOS may block broadcast packets from apps. Set the address to a host that relays magic packets to the node's network (a router, for example), or to the node's own address.")
        return TalosError(message: "\(reason). \(hint)")
    }

    /// Waits until the connection can send; a path that cannot (no route, local network access
    /// denied, policy) fails instead of waiting forever.
    private static func ready(_ connection: NWConnection) async throws {
        let gate = ResumeOnce()
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
            connection.stateUpdateHandler = { state in
                switch state {
                case .ready: gate.run { continuation.resume() }
                case .failed(let error), .waiting(let error): gate.run { continuation.resume(throwing: error) }
                case .cancelled: gate.run { continuation.resume(throwing: CancellationError()) }
                default: break
                }
            }
            connection.start(queue: DispatchQueue(label: "name.levis.ichor.wol"))
            DispatchQueue.global().asyncAfter(deadline: .now() + timeout) {
                gate.run { continuation.resume(throwing: NWError.posix(.ETIMEDOUT)) }
            }
        }
    }

    private static func send(_ payload: Data, on connection: NWConnection) async throws {
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
            connection.send(content: payload, completion: .contentProcessed { error in
                if let error { continuation.resume(throwing: error) } else { continuation.resume() }
            })
        }
    }
}

/// Runs the first closure given only: a continuation resumes once.
private final class ResumeOnce: @unchecked Sendable {
    private let lock = NSLock()
    private var done = false

    func run(_ body: () -> Void) {
        lock.lock()
        let first = !done
        done = true
        lock.unlock()
        if first { body() }
    }
}
