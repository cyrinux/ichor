import Darwin
import Foundation

/// A UDP socket allowed to send to a broadcast address (SO_BROADCAST), for Wake-on-LAN.
/// Network.framework has no broadcast support, hence BSD sockets. iOS only lets them through
/// with the com.apple.developer.networking.multicast entitlement (scripts/ios-build.sh,
/// ICHOR_IOS_MULTICAST); without it, sendto fails and the caller says to use a relay instead.
struct BroadcastSocket {
    private let descriptor: Int32

    /// A socket bound to the phone's Wi-Fi or Ethernet interface when [interface] is given,
    /// so a broadcast never leaves over cellular or a VPN tunnel.
    init(interface: String?) throws {
        descriptor = socket(AF_INET, SOCK_DGRAM, IPPROTO_UDP)
        guard descriptor >= 0 else { throw Self.posixError() }
        var on: Int32 = 1
        guard setsockopt(descriptor, SOL_SOCKET, SO_BROADCAST, &on, socklen_t(MemoryLayout<Int32>.size)) == 0 else {
            let error = Self.posixError()
            Darwin.close(descriptor)
            throw error
        }
        if let interface {
            var index = if_nametoindex(interface)
            if index != 0 {
                _ = setsockopt(descriptor, IPPROTO_IP, IP_BOUND_IF, &index, socklen_t(MemoryLayout<UInt32>.size))
            }
        }
    }

    /// Sends [payload] to the IPv4 [address] and [port].
    func send(_ payload: [UInt8], to address: String, port: UInt16) throws {
        var target = sockaddr_in()
        target.sin_len = UInt8(MemoryLayout<sockaddr_in>.size)
        target.sin_family = sa_family_t(AF_INET)
        target.sin_port = port.bigEndian
        guard inet_pton(AF_INET, address, &target.sin_addr) == 1 else { throw POSIXError(.EINVAL) }
        let sent = payload.withUnsafeBytes { bytes in
            withUnsafePointer(to: &target) { pointer in
                pointer.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                    sendto(descriptor, bytes.baseAddress, bytes.count, 0, $0, socklen_t(MemoryLayout<sockaddr_in>.size))
                }
            }
        }
        guard sent == payload.count else { throw Self.posixError() }
    }

    func close() { Darwin.close(descriptor) }

    private static func posixError() -> POSIXError {
        POSIXError(POSIXErrorCode(rawValue: errno) ?? .EIO)
    }
}
