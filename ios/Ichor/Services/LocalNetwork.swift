import Foundation
import IchorCore
import UIKit

/// The phone's own local networks, read from its interfaces (getifaddrs): what a network
/// search sweeps and where a Wake-on-LAN broadcast goes.
///
/// iOS asks the user once for local network access (NSLocalNetworkUsageDescription) on the
/// first connection to a LAN host, and offers no API to read the answer: when it was denied,
/// such connections fail (a node "does not answer"). Traffic through a VPN is not concerned.
enum LocalNetwork {
    /// Wi-Fi and wired Ethernet: en0 (Wi-Fi), en1… (USB/Thunderbolt Ethernet adapters). Not
    /// cellular (pdp_ip), VPN tunnels (utun, ipsec), AirDrop (awdl, llw), the hotspot bridge or loopback.
    private static func isLocal(_ name: String) -> Bool { name.hasPrefix("en") }

    /// The IPv4 and IPv6 addresses of the phone on its Wi-Fi and Ethernet networks.
    /// Link-local IPv6 addresses are left out: reaching a host on them needs a zone.
    static func addresses() -> [LocalAddress] {
        interfaceAddresses().compactMap { entry in
            guard isLocal(entry.name), !entry.address.hasPrefix("fe80:") else { return nil }
            return LocalAddress(entry.address, entry.prefixLength)
        }
    }

    /// The broadcast address of the phone's Wi-Fi or Ethernet IPv4 network (e.g. 192.168.1.255).
    static func directedBroadcastAddress() -> String? {
        for entry in interfaceAddresses() where isLocal(entry.name) {
            if let bytes = entry.ipv4, let broadcast = directedBroadcast(bytes, prefix: entry.prefixLength) { return broadcast }
        }
        return nil
    }

    /// The Wi-Fi or Ethernet interface holding the phone's IPv4 address (e.g. en0).
    static func ipv4Interface() -> String? {
        interfaceAddresses().first { isLocal($0.name) && $0.ipv4 != nil }?.name
    }

    /// Opens this app's page in the Settings app, where local network access can be allowed.
    @MainActor
    static func openSettings() {
        if let url = URL(string: UIApplication.openSettingsURLString) { UIApplication.shared.open(url) }
    }

    private struct InterfaceAddress {
        let name: String
        let address: String
        let prefixLength: Int
        /// The 4 bytes of an IPv4 address.
        let ipv4: [UInt8]?
    }

    /// Up, running interfaces' IPv4 and IPv6 addresses.
    private static func interfaceAddresses() -> [InterfaceAddress] {
        var head: UnsafeMutablePointer<ifaddrs>?
        guard getifaddrs(&head) == 0, let first = head else { return [] }
        defer { freeifaddrs(head) }
        var out: [InterfaceAddress] = []
        for pointer in sequence(first: first, next: { $0.pointee.ifa_next }) {
            let entry = pointer.pointee
            let flags = Int32(entry.ifa_flags)
            guard flags & IFF_UP != 0, flags & IFF_RUNNING != 0, flags & IFF_LOOPBACK == 0,
                  let addr = entry.ifa_addr, let mask = entry.ifa_netmask else { continue }
            let name = String(cString: entry.ifa_name)
            switch Int32(addr.pointee.sa_family) {
            case AF_INET:
                let ip = addr.withMemoryRebound(to: sockaddr_in.self, capacity: 1) { $0.pointee.sin_addr }
                let netmask = mask.withMemoryRebound(to: sockaddr_in.self, capacity: 1) { $0.pointee.sin_addr }
                let bytes = withUnsafeBytes(of: ip.s_addr) { Array($0) } // network order
                let prefix = UInt32(bigEndian: netmask.s_addr).nonzeroBitCount
                out.append(InterfaceAddress(name: name, address: bytes.map(String.init).joined(separator: "."),
                                            prefixLength: prefix, ipv4: bytes))
            case AF_INET6:
                var ip = addr.withMemoryRebound(to: sockaddr_in6.self, capacity: 1) { $0.pointee.sin6_addr }
                let netmask = mask.withMemoryRebound(to: sockaddr_in6.self, capacity: 1) { $0.pointee.sin6_addr }
                var buffer = [CChar](repeating: 0, count: Int(INET6_ADDRSTRLEN))
                guard inet_ntop(AF_INET6, &ip, &buffer, socklen_t(buffer.count)) != nil else { continue }
                let prefix = withUnsafeBytes(of: netmask) { $0.reduce(0) { $0 + $1.nonzeroBitCount } }
                let text = String(cString: buffer).split(separator: "%").first.map(String.init) ?? ""
                out.append(InterfaceAddress(name: name, address: text.lowercased(), prefixLength: prefix, ipv4: nil))
            default:
                continue
            }
        }
        return out
    }
}
