import Foundation

// Mirrors go/ichorgo/talos_vms.go (NodeVirtualMachines, ClusterVirtualMachines): the virtual
// machines the Talos hypervisor runs (Talos 1.15), read through the Talos API (os:reader).

/// One node's VMs, their disks and the node's content libraries.
public struct TalosNodeVMs: Decodable, Equatable, Hashable, Sendable {
    /// False on a node without the hypervisor resources (Talos older than 1.15), with `reason`.
    public let supported: Bool
    public let reason: String
    public let vms: [TalosVM]
    public let libraries: [TalosContentLibrary]
    /// Section (vms, specs, disks, libraries) to its error, for the sections that failed.
    public let errors: [String: String]

    public init(
        supported: Bool, reason: String = "", vms: [TalosVM] = [], libraries: [TalosContentLibrary] = [],
        errors: [String: String] = [:]
    ) {
        self.supported = supported
        self.reason = reason
        self.vms = vms
        self.libraries = libraries
        self.errors = errors
    }

    private enum CodingKeys: String, CodingKey { case supported, reason, vms, libraries, errors }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            supported: try c.field(.supported, false), reason: try c.field(.reason, ""), vms: try c.field(.vms, []),
            libraries: try c.field(.libraries, []), errors: try c.field(.errors, [:]))
    }

    /// Whether the node has something to show: the entries are hidden otherwise.
    public var hasVMs: Bool { supported && !vms.isEmpty }
}

public struct TalosVM: Decodable, Equatable, Hashable, Sendable {
    public let name: String
    /// Observed: unknown, running or stopped.
    public let powerState: String
    /// The spec's intent, "" without a spec.
    public let wantedPowerState: String
    /// unknown, pending, ready or error.
    public let stage: String
    public let error: String
    public let running: Bool
    public let cpus: Int
    public let memoryBytes: Int64
    /// "" is the builtin host profile.
    public let profile: String
    /// CIDRs.
    public let addresses: [String]
    public let interfaces: [TalosVMInterface]
    public let disks: [TalosVMDisk]

    private enum CodingKeys: String, CodingKey {
        case name, powerState, wantedPowerState, stage, error, running, cpus, memoryBytes, profile, addresses
        case interfaces, disks
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        powerState = try c.field(.powerState, "")
        wantedPowerState = try c.field(.wantedPowerState, "")
        stage = try c.field(.stage, "")
        error = try c.field(.error, "")
        running = try c.field(.running, false)
        cpus = try c.field(.cpus, 0)
        memoryBytes = try c.field(.memoryBytes, 0)
        profile = try c.field(.profile, "")
        addresses = try c.field(.addresses, [])
        interfaces = try c.field(.interfaces, [])
        disks = try c.field(.disks, [])
    }

    /// How the list groups it, with the KubeVirt badge colours: failing when it or a disk reports
    /// an error, changing while pending or not yet in the wanted state, else running or stopped.
    public var vmState: VmState {
        if stage == "error" || !error.isEmpty || disks.contains(where: { !$0.error.isEmpty }) { return .failing }
        if stage == "pending" || (!wantedPowerState.isEmpty && wantedPowerState != powerState) { return .changing }
        return running ? .running : .stopped
    }

    /// The observed state as the node writes it, "-" when unknown.
    public var stateLabel: String { powerState.isEmpty ? "-" : powerState }

    /// The first address without its prefix length, for the row.
    public var firstAddress: String? {
        addresses.first.map { String($0.split(separator: "/", maxSplits: 1).first ?? Substring($0)) }
    }
}

public struct TalosVMInterface: Decodable, Equatable, Hashable, Sendable {
    public let name: String
    public let mac: String
    public let addresses: [String]

    private enum CodingKeys: String, CodingKey { case name, mac, addresses }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        mac = try c.field(.mac, "")
        addresses = try c.field(.addresses, [])
    }
}

public struct TalosVMDisk: Decodable, Equatable, Hashable, Sendable {
    public let name: String
    public let pool: String
    public let volume: String
    public let format: String
    public let sizeBytes: Int64
    public let readOnly: Bool
    /// unknown, notReady, ready or observationUnavailable.
    public let phase: String
    public let error: String

    private enum CodingKeys: String, CodingKey { case name, pool, volume, format, sizeBytes, readOnly, phase, error }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        pool = try c.field(.pool, "")
        volume = try c.field(.volume, "")
        format = try c.field(.format, "")
        sizeBytes = try c.field(.sizeBytes, 0)
        readOnly = try c.field(.readOnly, false)
        phase = try c.field(.phase, "")
        error = try c.field(.error, "")
    }

    /// Not usable: an error, or not ready yet.
    public var bad: Bool { !error.isEmpty || phase == "notReady" }
}

public struct TalosContentLibrary: Decodable, Equatable, Hashable, Sendable {
    public let name: String
    public let path: String
    public let phase: String
    public let error: String

    private enum CodingKeys: String, CodingKey { case name, path, phase, error }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        path = try c.field(.path, "")
        phase = try c.field(.phase, "")
        error = try c.field(.error, "")
    }
}

/// One node of `TalosClusterVMs`: its report, or `error` when it could not be read.
public struct TalosNodeVMsEntry: Decodable, Equatable, Hashable, Sendable, Identifiable {
    public let node: String
    public let hostname: String
    public let report: TalosNodeVMs
    public let error: String

    public var id: String { node }
    /// The hostname, else the address.
    public var title: String { hostname.isEmpty ? node : hostname }

    public init(node: String, hostname: String, report: TalosNodeVMs, error: String = "") {
        self.node = node
        self.hostname = hostname
        self.report = report
        self.error = error
    }

    private enum CodingKeys: String, CodingKey { case node, hostname, error }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.field(.node, "")
        hostname = try c.field(.hostname, "")
        error = try c.field(.error, "")
        // The node's report is inlined beside node and hostname.
        report = try TalosNodeVMs(from: decoder)
    }
}

/// Every node's VMs, read in parallel; a node that does not answer carries an error.
public struct TalosClusterVMs: Decodable, Equatable, Hashable, Sendable {
    public let context: String
    public let nodes: [TalosNodeVMsEntry]

    public init(context: String = "", nodes: [TalosNodeVMsEntry]) {
        self.context = context
        self.nodes = nodes
    }

    private enum CodingKeys: String, CodingKey { case context, nodes }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        self.init(context: try c.field(.context, ""), nodes: try c.field(.nodes, []))
    }

    /// One node's report as a cluster of one, so the list and its sheet serve both.
    public static func ofNode(_ node: String, hostname: String, report: TalosNodeVMs) -> TalosClusterVMs {
        TalosClusterVMs(nodes: [TalosNodeVMsEntry(node: node, hostname: hostname, report: report)])
    }

    /// The rule of the entries: shown once a node reports the hypervisor and at least one VM.
    public var hasVMs: Bool { nodes.contains { $0.report.hasVMs } }

    /// The nodes the list shows, by hostname: those with VMs, those that failed, those whose
    /// Talos cannot tell (a 1.15 node next to older ones); a supported node without VMs is left out.
    public var shown: [TalosNodeVMsEntry] {
        nodes.filter { !$0.report.vms.isEmpty || !$0.error.isEmpty || !$0.report.supported }
            .sorted { $0.title < $1.title }
    }

    public var all: [TalosVM] { nodes.flatMap(\.report.vms) }

    /// Something is on its way: worth polling.
    public var busy: Bool { all.contains { $0.vmState == .changing } }
}

/// Running, stopped, failing and changing counts.
public struct TalosVMCounts: Equatable, Sendable {
    public let running: Int
    public let stopped: Int
    public let failing: Int
    public let changing: Int

    public init(running: Int, stopped: Int, failing: Int, changing: Int) {
        self.running = running
        self.stopped = stopped
        self.failing = failing
        self.changing = changing
    }

    public init(_ vms: [TalosVM]) {
        self.init(
            running: vms.filter { $0.vmState == .running }.count, stopped: vms.filter { $0.vmState == .stopped }.count,
            failing: vms.filter { $0.vmState == .failing }.count, changing: vms.filter { $0.vmState == .changing }.count
        )
    }
}
