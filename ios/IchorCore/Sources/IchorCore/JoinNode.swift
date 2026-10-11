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

/// Step 2: the new node's machine config, made from a member's (JoinNodeConfigPreview), with
/// its secrets hidden. The full config only goes to the node.
public struct JoinConfigPreview: Decodable, Equatable, Sendable {
    public let redactedYaml: String
    public let warnings: [String]
    public let needsBootstrap: Bool
    /// Bytes of the full config the node will receive.
    public let size: Int
    public let role: String
    public let hostname: String
    public let template: String

    private enum CodingKeys: String, CodingKey {
        case redactedYaml, warnings, needsBootstrap, size, role, hostname, template
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        redactedYaml = try c.field(.redactedYaml, "")
        warnings = try c.field(.warnings, [])
        needsBootstrap = try c.field(.needsBootstrap, false)
        size = try c.field(.size, 0)
        role = try c.field(.role, "")
        hostname = try c.field(.hostname, "")
        template = try c.field(.template, "")
    }
}

/// What a member's role makes of the new node: "init" (an old bootstrap node) joins as a control plane.
public func joinRole(_ role: String) -> String {
    role == "init" ? "controlplane" : role
}

/// The members a config can be taken from: those that answer and have a role, workers first.
public func joinTemplates(_ nodes: [NodeOverview]) -> [NodeOverview] {
    nodes
        .filter { $0.reachable && ["worker", "controlplane"].contains(joinRole($0.role)) }
        .sorted { a, b in
            let (aWorker, bWorker) = (joinRole(a.role) == "worker", joinRole(b.role) == "worker")
            return aWorker != bWorker ? aWorker : a.hostname < b.hostname
        }
}

/// "<role>-<n>" with the lowest n that no member's host name takes yet, from 1.
public func suggestedHostname(role: String, hostnames: [String]) -> String {
    let taken = Set(hostnames)
    var n = 1
    while taken.contains("\(joinRole(role))-\(n)") { n += 1 }
    return "\(joinRole(role))-\(n)"
}

/// The disks a config can install to: writable ones, by their device path.
public func installDisks(_ disks: [DiskInfo]) -> [String] {
    disks.filter { !$0.readonly && !$0.devPath.isEmpty }.map(\.devPath)
}
