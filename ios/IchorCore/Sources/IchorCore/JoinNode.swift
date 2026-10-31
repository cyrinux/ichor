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

/// `disk` is one of `node`'s writable disks, or the node listed none (a typed path then).
public func joinDiskFits(_ disk: String, node: MaintenanceInspection) -> Bool {
    let disks = installDisks(node.disks)
    return disks.isEmpty || disks.contains(disk)
}

/// What the user confirmed for the new node: the maintenance `address` and step 2's choices.
public struct JoinRequest: Equatable, Sendable {
    public let address: String
    public let template: String
    public let role: String
    public let hostname: String
    public let disk: String
    /// A prefix, or "" for DHCP.
    public let staticAddress: String
    /// The new node replaces a member discovery still lists under `hostname` (a dead node).
    public let replacing: Bool
    /// A server to create at Hetzner for the node, with the config as its user data (`address` unused).
    public let hetzner: HetznerCreateSpec?
    /// The Hetzner server joins Omni, not the cluster directly.
    public let omni: Bool
    /// No node at all: a throwaway server becomes a Talos snapshot (`hostname` only labels the run).
    public let snapshot: HetznerSnapshotSpec?
    /// A machine of Omni to put in the cluster (`hostname` and `role` are the machine's and the set's).
    public let omniAllocate: OmniAllocateSpec?

    public init(
        address: String, template: String, role: String, hostname: String, disk: String, staticAddress: String,
        replacing: Bool = false, hetzner: HetznerCreateSpec? = nil, omni: Bool = false,
        snapshot: HetznerSnapshotSpec? = nil, omniAllocate: OmniAllocateSpec? = nil
    ) {
        self.omniAllocate = omniAllocate
        self.snapshot = snapshot
        self.address = address
        self.template = template
        self.role = role
        self.hostname = hostname
        self.disk = disk
        self.staticAddress = staticAddress
        self.replacing = replacing
        self.hetzner = hetzner
        self.omni = omni
    }

    /// The steps of this run's timeline.
    public var phases: [JoinPhase] {
        if omniAllocate != nil { return JoinPhase.omniAllocateRun }
        if snapshot != nil { return JoinPhase.snapshotRun }
        guard hetzner != nil else { return JoinPhase.maintenanceRun }
        return omni ? JoinPhase.cloudOmniRun : JoinPhase.cloudRun
    }

    /// The same request, replacing (or not) the member already named `hostname`.
    public func withReplacing(_ on: Bool) -> JoinRequest {
        JoinRequest(
            address: address, template: template, role: role, hostname: hostname, disk: disk,
            staticAddress: staticAddress, replacing: on, hetzner: hetzner, omni: omni, snapshot: snapshot,
            omniAllocate: omniAllocate)
    }
}

/// One step of a node join (StartNodeJoin): `at` is unix ms, `address` where the node is followed.
public struct JoinProgress: Decodable, Equatable, Sendable {
    public let phase: String
    public let message: String
    public let at: Int64
    public let address: String

    public init(phase: String, message: String = "", at: Int64 = 0, address: String = "") {
        self.phase = phase
        self.message = message
        self.at = at
        self.address = address
    }

    private enum CodingKeys: String, CodingKey { case phase, message, at, address }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        phase = try c.field(.phase, "")
        message = try c.field(.message, "")
        at = try c.field(.at, 0)
        address = try c.field(.address, "")
    }
}

/// Phases of a node join, in order.
public enum JoinPhase: String, CaseIterable, Comparable, Sendable {
    case key, creating, starting, applying, allocated, installing, booting, rebooting, booted, joining, omni
    case registering, rescue, writing, snapshot, cleanup, done

    /// A node in maintenance mode that gets its config from the app.
    public static let maintenanceRun: [JoinPhase] = [
        .applying, .installing, .rebooting, .booted, .joining, .registering, .done,
    ]
    /// A server created with the config as its user data.
    public static let cloudRun: [JoinPhase] = [
        .creating, .starting, .rebooting, .booted, .joining, .registering, .done,
    ]
    /// A server created with Omni's join config.
    public static let cloudOmniRun: [JoinPhase] = [.creating, .starting, .omni, .done]
    /// A throwaway server that becomes a Talos snapshot.
    public static let snapshotRun: [JoinPhase] = [.key, .creating, .rescue, .writing, .snapshot, .cleanup, .done]
    /// A machine put in a cluster through Omni.
    public static let omniAllocateRun: [JoinPhase] = [.allocated, .installing, .booting, .done]

    public static func < (a: Self, b: Self) -> Bool {
        allCases.firstIndex(of: a)! < allCases.firstIndex(of: b)!
    }
}

/// A row of the join timeline.
public typealias JoinStep = TimelineStep<JoinPhase>

/// Timeline of the join from the events so far, like maintenanceTimeline: phases only move
/// forward, `finished` marks them all done, `failure` marks the furthest one failed.
public func joinTimeline(
    _ events: [JoinProgress], finished: Bool = false, failure: String? = nil,
    phases: [JoinPhase] = JoinPhase.maintenanceRun
) -> [JoinStep] {
    foldTimeline(
        events, phases: phases, finished: finished, failure: failure,
        phase: { event in JoinPhase(rawValue: event.phase).flatMap { phases.contains($0) ? $0 : nil } },
        at: \.at, message: \.message)
}
