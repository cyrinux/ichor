import Foundation

/// The clusters of `saved` (by fingerprint) still among `fingerprints`: a removed cluster's setting goes with it.
public func keepVpnOnly(saved: Set<String>, fingerprints: [String]) -> Set<String> {
    saved.intersection(fingerprints.filter { !$0.isEmpty })
}

/// Whether a call to the cluster `fingerprint` is held back: it is set to be reached over a
/// VPN only (`vpnOnly`) and none is up, so the call could only time out.
public func heldBackForVpn(vpnOnly: Set<String>, fingerprint: String?, vpnUp: Bool) -> Bool {
    guard !vpnUp, let fingerprint, !fingerprint.isEmpty else { return false }
    return vpnOnly.contains(fingerprint)
}

/// A network interface as the system path reports it (Network.framework's NWInterface).
public struct PathInterface: Equatable, Sendable {
    public let name: String
    /// Of type "other": what tunnels (VPNs) are, unlike Wi-Fi, cellular or wired Ethernet.
    public let isOther: Bool

    public init(name: String, isOther: Bool) {
        self.name = name
        self.isOther = isOther
    }
}

/// The name prefixes of the tunnel interfaces VPNs use on iOS: utun (IKEv2, WireGuard,
/// Tailscale, OpenVPN and every Network Extension VPN), ipsec (built-in IPsec), ppp (L2TP),
/// and tun/tap/wg for good measure.
private let vpnInterfacePrefixes = ["utun", "ipsec", "ppp", "tun", "tap", "wg"]

public func isVpnInterface(_ name: String) -> Bool {
    let lower = name.lowercased()
    return vpnInterfacePrefixes.contains { lower.hasPrefix($0) }
}

/// Whether a VPN is up, from the current network path: the default route goes through a
/// tunnel (`usesOther`, full-tunnel VPNs), or a tunnel interface is available (split-tunnel
/// VPNs like Tailscale, which only route their own networks). iOS has no public "VPN active"
/// API; this is what the path says, and a tunnel is only listed while it is connected.
public func vpnIsUp(interfaces: [PathInterface], usesOther: Bool) -> Bool {
    usesOther || interfaces.contains { $0.isOther && isVpnInterface($0.name) }
}
