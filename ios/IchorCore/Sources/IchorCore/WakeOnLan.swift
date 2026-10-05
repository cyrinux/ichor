import Foundation

/// Where magic packets go when no address is given: the phone's own network.
public let wolDefaultBroadcast = "255.255.255.255"

/// The discard port, the usual one for Wake-on-LAN (7 and 9 are both common).
public let wolDefaultPort = 9

/// How to wake one node: its MAC address and, optionally, where to send the magic packet (a
/// subnet's broadcast address, or a host that relays it) and on which UDP port. Kept on this
/// device only, like cluster names. Same rules as Android's model/WakeOnLan.kt.
public struct WolTarget: Codable, Equatable, Hashable, Sendable {
    /// Normalized, e.g. "aa:bb:cc:dd:ee:ff".
    public let mac: String
    /// Empty: `wolDefaultBroadcast`.
    public let broadcast: String
    public let port: Int

    public init(mac: String, broadcast: String = "", port: Int = wolDefaultPort) {
        self.mac = mac
        self.broadcast = broadcast
        self.port = port
    }

    public var address: String { broadcast.isEmpty ? wolDefaultBroadcast : broadcast }
}

/// What is wrong with a typed Wake-on-LAN setting.
public enum WolInputError: Error, Equatable, Sendable {
    case mac, broadcast, port
}

/// The six bytes of `input`, written with ':' or '-' separators, Cisco-style dots, or none
/// ("aa:bb:cc:dd:ee:ff", "AA-BB-CC-DD-EE-FF", "aabb.ccdd.eeff", "aabbccddeeff"); nil if it is
/// not a MAC address.
public func parseMac(_ input: String) -> [UInt8]? {
    let hex = input.trimmingCharacters(in: .whitespaces).filter { !":-. ".contains($0) }
    guard hex.count == 12, hex.allSatisfy({ $0.isASCII && $0.isHexDigit }) else { return nil }
    let chars = Array(hex)
    return (0..<6).compactMap { UInt8(String(chars[$0 * 2...$0 * 2 + 1]), radix: 16) }
}

/// `mac` as "aa:bb:cc:dd:ee:ff".
public func formatMac(_ mac: [UInt8]) -> String {
    mac.map { String(format: "%02x", $0) }.joined(separator: ":")
}

/// 6 × 0xFF, then the MAC 16 times: what a network card listens for to power its machine on.
public func magicPacket(_ mac: [UInt8]) -> [UInt8] {
    precondition(mac.count == 6, "a MAC address has 6 bytes")
    return Array(repeating: 0xFF, count: 6) + (0..<16).flatMap { _ in mac }
}

/// The broadcast address of the IPv4 subnet `address`/`prefix`, e.g. 192.168.1.20/24 → 192.168.1.255.
public func directedBroadcast(_ address: [UInt8], prefix: Int) -> String? {
    guard address.count == 4, (0...32).contains(prefix) else { return nil }
    let ip = address.reduce(UInt64(0)) { $0 << 8 | UInt64($1) }
    let host = (UInt64(1) << UInt64(32 - prefix)) - 1
    let broadcast = ip | host
    return (0..<4).reversed().map { String((broadcast >> UInt64($0 * 8)) & 0xFF) }.joined(separator: ".")
}

/// An IPv4 address or a host name to send to; blank is fine (the default broadcast).
public func isWolAddress(_ input: String) -> Bool {
    let text = input.trimmingCharacters(in: .whitespaces)
    if text.isEmpty { return true }
    if text.count > 253 { return false }
    let parts = text.split(separator: ".", omittingEmptySubsequences: false)
    if parts.allSatisfy({ !$0.isEmpty && $0.allSatisfy { $0.isASCII && $0.isNumber } }) {
        return parts.count == 4 && parts.allSatisfy { $0.count <= 3 && (Int($0) ?? 256) <= 255 }
    }
    return parts.allSatisfy(isHostLabel)
}

/// [A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?
private func isHostLabel(_ label: Substring) -> Bool {
    func alnum(_ c: Character) -> Bool { c.isASCII && (c.isLetter || c.isNumber) }
    guard let first = label.first, let last = label.last, label.count <= 63, alnum(first), alnum(last) else { return false }
    return label.allSatisfy { alnum($0) || $0 == "-" }
}

/// The typed fields as a `WolTarget`, or what is wrong with them. Blank port: `wolDefaultPort`.
public func parseWolTarget(mac: String, broadcast: String, port: String) -> Result<WolTarget, WolInputError> {
    guard let bytes = parseMac(mac) else { return .failure(.mac) }
    guard isWolAddress(broadcast) else { return .failure(.broadcast) }
    let trimmedPort = port.trimmingCharacters(in: .whitespaces)
    let number = trimmedPort.isEmpty ? wolDefaultPort : Int(trimmedPort)
    guard let number, (1...65535).contains(number) else { return .failure(.port) }
    return .success(WolTarget(mac: formatMac(bytes), broadcast: broadcast.trimmingCharacters(in: .whitespaces), port: number))
}

/// The store key of `node` (its talosconfig address) in the cluster `fingerprint`.
public func wolKey(fingerprint: String, node: String) -> String { "\(fingerprint)|\(node)" }

/// How a `WolTarget` is stored: "mac|broadcast|port" (none of them can hold a '|'), as on Android.
public func encodeWolTarget(_ target: WolTarget) -> String { "\(target.mac)|\(target.broadcast)|\(target.port)" }

/// The `WolTarget` `encodeWolTarget` stored as `value`; nil if it is not one.
public func decodeWolTarget(_ value: String) -> WolTarget? {
    let parts = value.split(separator: "|", omittingEmptySubsequences: false).map(String.init)
    guard parts.count == 3, let bytes = parseMac(parts[0]), let port = Int(parts[2]), (1...65535).contains(port) else { return nil }
    return WolTarget(mac: formatMac(bytes), broadcast: parts[1], port: port)
}

/// The settings of `saved` (by `wolKey`) whose cluster is still among `fingerprints`.
public func keepWolTargets<T>(_ saved: [String: T], fingerprints: [String]) -> [String: T] {
    let known = Set(fingerprints.filter { !$0.isEmpty })
    return saved.filter { key, _ in known.contains(String(key.prefix { $0 != "|" })) }
}

/// A physical link of a node and its MAC address, as last seen while the node was up.
public struct SeenMac: Codable, Equatable, Hashable, Sendable {
    public let link: String
    public let mac: String

    public init(link: String, mac: String) {
        self.link = link
        self.mac = mac
    }
}

/// The links of a node as they are remembered: "eth0=aa:bb:…,eth1=…".
public func encodeSeenMacs(_ seen: [SeenMac]) -> String {
    seen.map { "\($0.link)=\($0.mac)" }.joined(separator: ",")
}

/// What `encodeSeenMacs` stored; entries that are not a link and a MAC are left out.
public func decodeSeenMacs(_ value: String) -> [SeenMac] {
    value.split(separator: ",").compactMap { entry in
        guard let equals = entry.firstIndex(of: "=") else { return nil }
        let link = entry[..<equals].trimmingCharacters(in: .whitespaces)
        guard !link.isEmpty, let mac = parseMac(String(entry[entry.index(after: equals)...])) else { return nil }
        return SeenMac(link: link, mac: formatMac(mac))
    }
}

/// The MAC addresses a node reports for its physical Ethernet links, to pick one from.
public func wolCandidates(_ links: [NetLink]) -> [NetLink] {
    var seen = Set<[UInt8]>()
    return links.filter { link in
        guard !link.virtual, link.kind.isEmpty, link.type == "ether", let mac = parseMac(link.hardwareAddr) else { return false }
        return seen.insert(mac).inserted
    }
}

/// The MACs worth remembering from a node's `links`: those of its physical Ethernet links.
public func seenMacs(_ links: [NetLink]) -> [SeenMac] {
    wolCandidates(links).compactMap { link in parseMac(link.hardwareAddr).map { SeenMac(link: link.name, mac: formatMac($0)) } }
}

/// Where "Wake" sends magic packets: the `saved` setting; else, so a node that went down
/// before anyone set it up can still be woken, every MAC it was `seen` with, on the phone's
/// own network (a packet for a card that is not wired is simply lost).
public func wakeTargets(saved: WolTarget?, seen: [SeenMac]) -> [WolTarget] {
    if let saved { return [saved] }
    var out: [WolTarget] = []
    for mac in seen where !out.contains(WolTarget(mac: mac.mac)) { out.append(WolTarget(mac: mac.mac)) }
    return out
}
