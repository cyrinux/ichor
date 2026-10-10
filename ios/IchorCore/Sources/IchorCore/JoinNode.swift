import Foundation

/// What a node in maintenance mode (booted from the Talos ISO, not installed) says about itself,
/// from the Go core (MaintenanceNodeInspect); each section is best-effort and a failed one is
/// named in `errors` (system, disks, links, addresses).
public struct MaintenanceInspection: Decodable, Equatable, Sendable {
    public let address: String
    public let version: String
    public let arch: String
    public let platform: String
    /// nil when unavailable.
    public let system: SystemInfo?
    public let disks: [DiskInfo]
    /// Physical links only.
    public let links: [NetLink]
    public let addresses: [NetAddress]
    /// False for a node already installed: it asks for a client certificate, and nothing was read.
    public let maintenance: Bool
    public let errors: [String: String]

    private enum CodingKeys: String, CodingKey {
        case address, version, arch, platform, system, disks, links, addresses, maintenance, errors
    }

    // Go encodes empty (nil) slices and maps as null.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        address = try c.field(.address, "")
        version = try c.field(.version, "")
        arch = try c.field(.arch, "")
        platform = try c.field(.platform, "")
        system = try c.decodeIfPresent(SystemInfo.self, forKey: .system)
        disks = try c.field(.disks, [])
        links = try c.field(.links, [])
        addresses = try c.field(.addresses, [])
        maintenance = try c.field(.maintenance, false)
        errors = try c.field(.errors, [:])
    }

    /// The addresses of `link`, as prefixes ("192.168.1.20/24").
    public func addresses(on link: String) -> [String] {
        addresses.filter { $0.link == link }.map(\.address)
    }
}

/// The address as typed, ready to send: no blanks; empty when there is nothing to inspect.
public func joinAddress(_ typed: String) -> String {
    typed.trimmingCharacters(in: .whitespacesAndNewlines)
}
