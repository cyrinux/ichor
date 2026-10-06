import Foundation

/// One captured packet as summarized by Go (StartPacketCapture's OnPacket, ReadPcap).
public struct PacketSummary: Decodable, Equatable, Identifiable, Sendable {
    /// 0-based position in the capture file (PacketDetail's index); live summaries skip
    /// numbers, Go sends at most ~20 per second.
    public let n: Int
    /// Unix ms.
    public let ts: Int64
    /// Bytes on the wire.
    public let len: Int
    public let src: String
    public let dst: String
    public let proto: String
    public let info: String

    public var id: Int { n }
    /// 1-based, as shown.
    public var number: Int { n + 1 }

    public init(n: Int, ts: Int64 = 0, len: Int = 0, src: String = "", dst: String = "", proto: String = "", info: String = "") {
        self.n = n
        self.ts = ts
        self.len = len
        self.src = src
        self.dst = dst
        self.proto = proto
        self.info = info
    }

    private enum CodingKeys: String, CodingKey { case n, ts, len, src, dst, proto, info }

    // Go omits or nulls empty fields.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        n = try c.decode(Int.self, forKey: .n)
        ts = try c.field(.ts, 0)
        len = try c.field(.len, 0)
        src = try c.field(.src, "")
        dst = try c.field(.dst, "")
        proto = try c.field(.proto, "")
        info = try c.field(.info, "")
    }

    /// Color family of the protocol tag.
    public var protoKind: ProtoKind { ProtoKind(proto) }
}

public enum ProtoKind: Equatable, Sendable {
    case tcp, udp, icmp, dns, arp, tls, other

    public init(_ proto: String) {
        switch proto.uppercased() {
        case "DNS", "MDNS", "LLMNR": self = .dns
        case "TLS", "HTTP": self = .tls
        case let p where p.hasPrefix("TCP"): self = .tcp
        case let p where p.hasPrefix("UDP"): self = .udp
        case let p where p.hasPrefix("ICMP"): self = .icmp
        case "ARP": self = .arp
        default: self = .other
        }
    }
}

/// A page of a capture file (ReadPcap): `total` packets in the file.
public struct PcapPage: Decodable, Equatable, Sendable {
    public let packets: [PacketSummary]
    public let total: Int

    public init(packets: [PacketSummary], total: Int) {
        self.packets = packets
        self.total = total
    }

    private enum CodingKeys: String, CodingKey { case packets, total }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        packets = try c.field(.packets, [])
        total = try c.field(.total, packets.count)
    }
}

/// Decoded layers of one packet (PacketDetail).
public struct PacketDetail: Decodable, Equatable, Sendable {
    public struct Field: Decodable, Equatable, Sendable {
        public let k: String
        public let v: String

        public init(k: String, v: String) {
            self.k = k
            self.v = v
        }
    }

    public struct Layer: Decodable, Equatable, Identifiable, Sendable {
        public let name: String
        public let fields: [Field]

        public var id: String { name }

        public init(name: String, fields: [Field]) {
            self.name = name
            self.fields = fields
        }

        private enum CodingKeys: String, CodingKey { case name, fields }

        public init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            name = try c.field(.name, "")
            fields = try c.field(.fields, [])
        }
    }

    public let layers: [Layer]
    public let hex: String

    public init(layers: [Layer], hex: String) {
        self.layers = layers
        self.hex = hex
    }

    private enum CodingKeys: String, CodingKey { case layers, hex }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        layers = try c.field(.layers, [])
        hex = try c.field(.hex, "")
    }
}

/// Packets per page when reopening a capture file.
public let pcapPageSize = 500

/// Live list cap: older rows are dropped from the screen (the file keeps them).
public let liveCaptureRows = 2_000

/// Appends packets to the live list, keeping the newest `limit`.
public func appendPackets(_ list: [PacketSummary], _ new: [PacketSummary], limit: Int = liveCaptureRows) -> [PacketSummary] {
    let all = list + new
    return all.count > limit ? Array(all.suffix(limit)) : all
}

public enum CaptureDuration: Int, CaseIterable, Identifiable, Sendable {
    case s10 = 10, s30 = 30, s60 = 60, m5 = 300

    public var id: Int { rawValue }
    public var seconds: Int { rawValue }
}

public enum CaptureSizeLimit: Int64, CaseIterable, Identifiable, Sendable {
    case mib5 = 5, mib20 = 20, mib100 = 100

    public var id: Int64 { rawValue }
    public var bytes: Int64 { rawValue * 1_048_576 }
}

/// Filter presets (BPF, as tcpdump). No Talos API preset: Go always excludes port 50000,
/// so the capture does not record its own stream.
public struct CapturePreset: Equatable, Identifiable, Sendable {
    public let name: String
    public let filter: String

    public var id: String { name }

    public static let all: [CapturePreset] = [
        CapturePreset(name: "DNS", filter: "udp port 53"),
        CapturePreset(name: "ICMP", filter: "icmp or icmp6"),
        CapturePreset(name: "HTTPS", filter: "tcp port 443"),
        CapturePreset(name: "Kubernetes API", filter: "tcp port 6443"),
    ]
}

/// Everything StartPacketCapture needs besides the node.
public struct CaptureOptions: Equatable, Sendable {
    public var interface: String
    public var filter: String
    public var promiscuous: Bool
    public var duration: CaptureDuration
    public var sizeLimit: CaptureSizeLimit
    /// Bytes kept per packet, 0 = whole packets.
    public var snapLen: Int

    public init(interface: String = "", filter: String = "", promiscuous: Bool = false,
                duration: CaptureDuration = .s30, sizeLimit: CaptureSizeLimit = .mib20, snapLen: Int = 0) {
        self.interface = interface
        self.filter = filter
        self.promiscuous = promiscuous
        self.duration = duration
        self.sizeLimit = sizeLimit
        self.snapLen = snapLen
    }

    public var trimmedFilter: String { filter.trimmingCharacters(in: .whitespacesAndNewlines) }
}

public enum CaptureOptionsProblem: Equatable, Sendable {
    case noInterface
    /// Go's message for an invalid BPF expression.
    case invalidFilter(String)
    /// Validation still running for the current filter.
    case checkingFilter
}

/// Why the capture cannot start yet; nil when it can. `filterError` is ValidateCaptureFilter's
/// answer for `validatedFilter` ("" = valid); a stale answer counts as still checking.
public func captureOptionsProblem(_ options: CaptureOptions, validatedFilter: String?, filterError: String) -> CaptureOptionsProblem? {
    if options.interface.trimmingCharacters(in: .whitespaces).isEmpty { return .noInterface }
    let filter = options.trimmedFilter
    if filter.isEmpty { return nil }
    guard validatedFilter == filter else { return .checkingFilter }
    return filterError.isEmpty ? nil : .invalidFilter(filterError)
}

/// Interfaces offered for capture: physical ones first, then up before down, then by name;
/// virtual (pod/CNI) links only when asked. Loopback is kept, after the rest.
public func captureInterfaces(_ links: [NetLink], includeVirtual: Bool) -> [NetLink] {
    func rank(_ link: NetLink) -> Int {
        if link.type == "loopback" || link.name == "lo" { return 3 }
        return (link.virtual || !link.kind.isEmpty ? 1 : 0) + (link.isUp ? 0 : 1)
    }
    return links
        .filter { includeVirtual || !$0.virtual }
        .sorted { a, b in
            let (ra, rb) = (rank(a), rank(b))
            return ra != rb ? ra < rb : a.name < b.name
        }
}

/// "<host>-<iface>-<yyyyMMdd-HHmmss>.pcap", with characters that are awkward in file names
/// replaced by "-".
public func captureFilename(hostname: String, interface: String, date: Date, timeZone: TimeZone = .current) -> String {
    return "\(captureFileSafe(hostname, fallback: "node"))-\(captureFileSafe(interface, fallback: "any"))-\(fileTimestamp(date, format: "yyyyMMdd-HHmmss", timeZone: timeZone)).pcap"
}

private func captureFileSafe(_ text: String, fallback: String) -> String {
    let allowed = CharacterSet(charactersIn: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-")
    let mapped = text.unicodeScalars.map { allowed.contains($0) ? String($0) : "-" }.joined()
    return mapped.isEmpty ? fallback : mapped
}

/// A capture file on the phone.
/// A capture kept on the phone.
public typealias CaptureFile = LocalFile

/// Newest first; only .pcap files.
public func sortCaptureFiles(_ files: [CaptureFile]) -> [CaptureFile] {
    files.filter { $0.name.hasSuffix(".pcap") }
        .sorted { $0.modified != $1.modified ? $0.modified > $1.modified : $0.name > $1.name }
}

public func totalCaptureSize(_ files: [CaptureFile]) -> Int64 {
    files.reduce(Int64(0)) { $0 &+ max($1.size, 0) }
}

/// "01:05": minutes and seconds elapsed in a capture (at most 5 min).
public func formatElapsed(_ seconds: Int) -> String {
    let s = max(seconds, 0)
    return String(format: "%02d:%02d", s / 60, s % 60)
}
