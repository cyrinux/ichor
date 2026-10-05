import Foundation

/// A host found on the network that answered the Talos API with the credentials of `contexts`
/// (Go FindEndpoints).
public struct EndpointMatch: Decodable, Equatable, Hashable, Sendable {
    public let endpoint: String
    public let hostname: String
    public let version: String
    /// Machine type: controlplane, init or worker ("" when unknown).
    public let role: String
    public let contexts: [String]

    public init(endpoint: String, hostname: String = "", version: String = "", role: String = "", contexts: [String] = []) {
        self.endpoint = endpoint
        self.hostname = hostname
        self.version = version
        self.role = role
        self.contexts = contexts
    }

    private enum CodingKeys: String, CodingKey { case endpoint, hostname, version, role, contexts }

    // Go encodes empty (nil) slices as null.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        endpoint = try c.decode(String.self, forKey: .endpoint)
        hostname = try c.decodeIfPresent(String.self, forKey: .hostname) ?? ""
        version = try c.decodeIfPresent(String.self, forKey: .version) ?? ""
        role = try c.decodeIfPresent(String.self, forKey: .role) ?? ""
        contexts = try c.decodeIfPresent([String].self, forKey: .contexts) ?? []
    }
}

/// What an endpoint said about itself when tested with a context's credentials (Go ProbeEndpoint).
public struct EndpointProbe: Decodable, Equatable, Sendable {
    public let endpoint: String
    public let hostname: String
    public let version: String
    public let role: String

    public init(endpoint: String, hostname: String = "", version: String = "", role: String = "") {
        self.endpoint = endpoint
        self.hostname = hostname
        self.version = version
        self.role = role
    }

    private enum CodingKeys: String, CodingKey { case endpoint, hostname, version, role }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        endpoint = try c.decodeIfPresent(String.self, forKey: .endpoint) ?? ""
        hostname = try c.decodeIfPresent(String.self, forKey: .hostname) ?? ""
        version = try c.decodeIfPresent(String.self, forKey: .version) ?? ""
        role = try c.decodeIfPresent(String.self, forKey: .role) ?? ""
    }

    /// "hostname · version", what is known of them.
    public var summary: String { [hostname, version].filter { !$0.isEmpty }.joined(separator: " · ") }
}

/// An IPv4 or IPv6 address of the phone on a local network, with its prefix length.
public struct LocalAddress: Equatable, Sendable {
    public let address: String
    public let prefixLength: Int

    public init(_ address: String, _ prefixLength: Int) {
        self.address = address
        self.prefixLength = prefixLength
    }
}

/// Hosts a scan covers at most, as the Go side enforces: a few seconds on a phone.
public let maxScanHosts = 4096

/// The longest endpoint the editor takes.
public let endpointMax = 260

/// The phone's own network is scanned up to this size (a /22) around its address.
private let widestLocalPrefix = 22
private let neighbourPrefix = 24
/// An IPv6 network is swept as a /120 (256 hosts): a /64 holds far too many addresses. The
/// Go side takes up to a /116.
private let ipv6ScanPrefix = 120

/// The /24s home and lab routers hand out by default, tried after the phone's own network
/// and those of the talosconfig: a node shared from elsewhere is usually on one of them.
/// The private ranges as a whole (10/8 alone is 16 million hosts) are far too large to sweep.
public let commonPrivateNetworks = [
    "192.168.0.0/24",
    "192.168.1.0/24",
    "192.168.2.0/24",
    "192.168.178.0/24",
    "10.0.0.0/24",
    "10.0.1.0/24",
    "172.16.0.0/24",
]

/// The networks to scan for Talos nodes, in order and within `maxScanHosts`: the phone's own
/// networks (`local`: at most a /22 around an IPv4 address; the first /120 of an IPv6 prefix,
/// where statically numbered nodes sit, SLAAC addresses being random), the neighbourhood of
/// each private address the talosconfig lists (`known` endpoints and nodes, a port allowed:
/// its IPv4 /24 or IPv6 /120), then `commonPrivateNetworks`. Only private ranges (RFC 1918,
/// CGNAT, IPv6 unique local fc00::/7): never the Internet. Same as Android's scanNetworks.
public func scanNetworks(local: [LocalAddress], known: [String]) -> [String] {
    let own: [IPNetwork] = local.compactMap { address in
        guard let ip = parseIP(address.address), isPrivate(ip) else { return nil }
        if ip.width == 32 {
            return network(ip, min(max(address.prefixLength, widestLocalPrefix), 32))
        }
        return network(network(ip, min(max(address.prefixLength, 0), ipv6ScanPrefix)).base, ipv6ScanPrefix)
    }
    let listed: [IPNetwork] = known.compactMap { endpoint in
        guard let ip = parseIP(hostOf(endpoint)), isPrivate(ip) else { return nil }
        return network(ip, ip.width == 32 ? neighbourPrefix : ipv6ScanPrefix)
    }
    let common: [IPNetwork] = commonPrivateNetworks.compactMap(parseNetwork)

    // A network inside one already listed adds nothing.
    var picked: [IPNetwork] = []
    var hosts = 0
    for net in own + listed + common {
        let covered = picked.contains { $0.contains(net) }
        if covered || hosts + net.hostCount > maxScanHosts { continue }
        picked.append(net)
        hosts += net.hostCount
    }
    return picked.map(\.description)
}

/// The address or hostname of an endpoint, without its port (a bare IPv6 address left as is).
public func hostOf(_ endpoint: String) -> String {
    if endpoint.hasPrefix("[") {
        let inside = endpoint.dropFirst()
        return String(inside.prefix { $0 != "]" })
    }
    guard let colon = endpoint.firstIndex(of: ":"), colon > endpoint.startIndex else { return endpoint }
    let rest = endpoint[endpoint.index(after: colon)...]
    return rest.contains(":") ? endpoint : String(endpoint[..<colon])
}

/// Whether `text` can be one endpoint of a list: not blank, no separator. The Go side checks
/// the rest (address or hostname, optional port) when testing or saving, and says what is wrong.
public func isEndpoint(_ text: String) -> Bool {
    !text.isEmpty && text.count <= endpointMax && !text.contains { $0.isWhitespace || $0 == "," }
}

// MARK: - Addresses

/// An IPv4 (4 bytes) or IPv6 (16 bytes) address.
private struct IPAddress: Equatable {
    var bytes: [UInt8]
    var width: Int { bytes.count * 8 }
}

/// The network of `base` (host bits zero) with a `bits`-long prefix.
private struct IPNetwork: Equatable, CustomStringConvertible {
    let base: IPAddress
    let bits: Int

    /// Only small networks are counted (a /22 or a /120 at most).
    var hostCount: Int { 1 << min(base.width - bits, 62) }

    func contains(_ other: IPNetwork) -> Bool {
        base.width == other.base.width && bits <= other.bits && network(other.base, bits).base == base
    }

    var description: String {
        let text = base.width == 32 ? base.bytes.map(String.init).joined(separator: ".") : formatIPv6(base.bytes)
        return "\(text)/\(bits)"
    }
}

private func network(_ ip: IPAddress, _ bits: Int) -> IPNetwork {
    var bytes = ip.bytes
    for index in bytes.indices {
        let start = index * 8
        if start >= bits {
            bytes[index] = 0
        } else if start + 8 > bits {
            bytes[index] &= UInt8(truncatingIfNeeded: 0xFF << (8 - (bits - start)))
        }
    }
    return IPNetwork(base: IPAddress(bytes: bytes), bits: bits)
}

private func parseNetwork(_ cidr: String) -> IPNetwork? {
    let parts = cidr.split(separator: "/")
    guard parts.count == 2, let ip = parseIP(String(parts[0])), let bits = Int(parts[1]) else { return nil }
    return network(ip, bits)
}

private let privateNetworks: [IPNetwork] = ["10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "100.64.0.0/10", "fc00::/7"]
    .compactMap(parseNetwork)

private func isPrivate(_ ip: IPAddress) -> Bool {
    privateNetworks.contains { $0.contains(network(ip, ip.width)) }
}

/// A literal address (an IPv6 one without a zone: a link-local address is not swept), or nil.
private func parseIP(_ text: String) -> IPAddress? {
    if let v4 = parseIPv4(text) { return IPAddress(bytes: v4) }
    if let v6 = parseIPv6(text) { return IPAddress(bytes: v6) }
    return nil
}

private func parseIPv4(_ text: String) -> [UInt8]? {
    let parts = text.split(separator: ".", omittingEmptySubsequences: false)
    guard parts.count == 4 else { return nil }
    var bytes: [UInt8] = []
    for part in parts {
        guard !part.isEmpty, part.count <= 3, part.allSatisfy({ $0.isASCII && $0.isNumber }),
              let octet = Int(part), octet <= 255 else { return nil }
        bytes.append(UInt8(octet))
    }
    return bytes
}

private func parseIPv6(_ text: String) -> [UInt8]? {
    let halves = text.components(separatedBy: "::")
    guard text.contains(":"), halves.count <= 2 else { return nil }
    // An embedded IPv4 address ends the address: never before "::".
    if halves.count == 2 && !halves[1].isEmpty && halves[0].contains(".") { return nil }
    guard let head = ipv6Groups(halves[0]) else { return nil }
    var tail: [UInt16] = []
    if halves.count == 2 {
        guard let groups = ipv6Groups(halves[1]) else { return nil }
        tail = groups
    }
    let missing = 8 - head.count - tail.count
    if halves.count == 1 ? missing != 0 : missing < 1 { return nil }
    let groups = head + Array(repeating: 0, count: missing) + tail
    return groups.flatMap { [UInt8($0 >> 8), UInt8($0 & 0xFF)] }
}

/// The 16-bit groups of one side of "::" ("" has none), a trailing dotted IPv4 counting as two.
private func ipv6Groups(_ part: String) -> [UInt16]? {
    if part.isEmpty { return [] }
    let fields = part.split(separator: ":", omittingEmptySubsequences: false)
    var groups: [UInt16] = []
    for (index, field) in fields.enumerated() {
        if index == fields.count - 1 && field.contains(".") {
            guard let v4 = parseIPv4(String(field)) else { return nil }
            groups.append(UInt16(v4[0]) << 8 | UInt16(v4[1]))
            groups.append(UInt16(v4[2]) << 8 | UInt16(v4[3]))
        } else {
            guard !field.isEmpty, field.count <= 4, field.allSatisfy({ $0.isHexDigit && $0.isASCII }),
                  let value = UInt16(field, radix: 16) else { return nil }
            groups.append(value)
        }
    }
    return groups
}

/// RFC 5952 text, as Go writes it: lower case, the first longest run of zero groups as "::".
private func formatIPv6(_ bytes: [UInt8]) -> String {
    let groups = stride(from: 0, to: 16, by: 2).map { UInt16(bytes[$0]) << 8 | UInt16(bytes[$0 + 1]) }
    let hex = groups.map { String($0, radix: 16) }
    var best: (start: Int, length: Int)?
    var index = 0
    while index < groups.count {
        guard groups[index] == 0 else {
            index += 1
            continue
        }
        let start = index
        while index < groups.count && groups[index] == 0 { index += 1 }
        let length = index - start
        if length >= 2 && length > (best?.length ?? 0) { best = (start, length) }
    }
    guard let best else { return hex.joined(separator: ":") }
    return hex[..<best.start].joined(separator: ":") + "::" + hex[(best.start + best.length)...].joined(separator: ":")
}
