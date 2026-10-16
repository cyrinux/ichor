import Foundation

// Mirrors go/ichorgo/kube_virt.go (KubeVirtualMachines) and kube_virt_actions.go (KubeVMAction).

/// The KubeVirt (and Harvester) virtual machines of the cluster, failing ones first.
public struct VirtualMachines: Decodable, Equatable, Sendable {
    /// False: the cluster does not serve kubevirt.io (no error).
    public let installed: Bool
    public let harvester: Bool
    public let version: String
    /// A list that failed; what could be read still shows.
    public let error: String
    public let vms: [VirtualMachine]
    public let namespaces: [String]
    public let counts: VmCounts

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        installed = try c.field(.installed, false)
        harvester = try c.field(.harvester, false)
        version = try c.field(.version, "")
        error = try c.field(.error, "")
        vms = try c.field(.vms, [])
        namespaces = try c.field(.namespaces, [])
        counts = try c.field(.counts, VmCounts())
    }

    private enum CodingKeys: String, CodingKey { case installed, harvester, version, error, vms, namespaces, counts }

    /// Something is on its way (starting, stopping, migrating, importing): worth polling.
    public var busy: Bool { vms.contains { $0.vmState == .changing || $0.progress != nil } }

    /// The VMs of `namespace`, every one when nil.
    public func inNamespace(_ namespace: String?) -> [VirtualMachine] {
        guard let namespace else { return vms }
        return vms.filter { $0.namespace == namespace }
    }
}

public struct VmCounts: Decodable, Equatable, Sendable {
    public let running: Int
    public let stopped: Int
    public let failing: Int
    public let migrating: Int
    public let importing: Int

    public init(running: Int = 0, stopped: Int = 0, failing: Int = 0, migrating: Int = 0, importing: Int = 0) {
        self.running = running
        self.stopped = stopped
        self.failing = failing
        self.migrating = migrating
        self.importing = importing
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        self.init(running: try c.field(.running, 0), stopped: try c.field(.stopped, 0), failing: try c.field(.failing, 0),
                  migrating: try c.field(.migrating, 0), importing: try c.field(.importing, 0))
    }

    private enum CodingKeys: String, CodingKey { case running, stopped, failing, migrating, importing }
}

public struct VirtualMachine: Decodable, Equatable, Identifiable, Hashable, Sendable {
    public let namespace: String
    public let name: String
    /// Always, Halted, Manual, RerunOnFailure, Once.
    public let runStrategy: String
    /// KubeVirt's printable status as written (Running, Stopped, CrashLoopBackOff…).
    public let status: String
    /// The Go core's grouping of `status`.
    public let vmState: VmState
    public let ready: Bool
    public let created: Bool
    /// Start or Stop, "" when none.
    public let pendingChange: String
    public let conditions: [VmCondition]
    /// vCPUs, 0 when the template sets none.
    public let cpu: Int
    /// As written ("4Gi"), "" when unset.
    public let memory: String
    public let node: String
    /// The instance's phase, "" without one.
    public let phase: String
    public let ips: [String]
    public let guestOS: String
    public let launcherImage: String
    /// The newest migration: in flight, or finished less than an hour ago.
    public let migration: VmMigration?
    public let disks: [VmDisk]

    public var id: String { "\(namespace)/\(name)" }
    public var firstIP: String? { ips.first }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        runStrategy = try c.field(.runStrategy, "")
        status = try c.field(.status, "")
        vmState = VmState(rawValue: try c.field(.state, "")) ?? .changing
        ready = try c.field(.ready, false)
        created = try c.field(.created, false)
        pendingChange = try c.field(.pendingChange, "")
        conditions = try c.field(.conditions, [])
        cpu = try c.field(.cpu, 0)
        memory = try c.field(.memory, "")
        node = try c.field(.node, "")
        phase = try c.field(.phase, "")
        ips = try c.field(.ips, [])
        guestOS = try c.field(.guestOS, "")
        launcherImage = try c.field(.launcherImage, "")
        migration = try c.decodeIfPresent(VmMigration.self, forKey: .migration)
        disks = try c.field(.disks, [])
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, runStrategy, status, state, ready, created, pendingChange, conditions, cpu, memory
        case node, phase, ips, guestOS, launcherImage, migration, disks
    }

    private var migrationInFlight: Bool { migration.map { !$0.completed && !$0.failed } ?? false }

    /// A disk import or a migration in flight, the import first (the VM cannot boot without it).
    public var progress: VmProgress? {
        if let disk = disks.first(where: \.importing) { return .importing(disk: disk.name, progress: disk.progress) }
        if let m = migration, migrationInFlight { return .migrating(from: m.sourceNode, to: m.targetNode) }
        return nil
    }

    /// Whether `action` may be asked in this state, as the Go core's vmRefusal answers; start is
    /// also kept off while the instance runs (migrating, paused) or is going away (stopping).
    public func allows(_ action: VmAction) -> Bool {
        switch action {
        case .start: !["Running", "Starting", "Provisioning", "Migrating", "Paused", "Stopping"].contains(status)
        case .stop: status != "Stopped" && status != "Stopping"
        case .restart: runStrategy != "Halted" && status == "Running"
        case .migrate: !migrationInFlight && status == "Running"
        case .pause: status == "Running" || status == "Migrating"
        case .unpause: status == "Paused"
        }
    }
}

public struct VmCondition: Decodable, Equatable, Hashable, Sendable {
    public let type: String
    public let status: String
    public let reason: String
    public let message: String

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        type = try c.field(.type, "")
        status = try c.field(.status, "")
        reason = try c.field(.reason, "")
        message = try c.field(.message, "")
    }

    private enum CodingKeys: String, CodingKey { case type, status, reason, message }

    /// Failure set, or Ready unset: worth the red.
    public var bad: Bool { (type == "Failure" && status == "True") || (type == "Ready" && status == "False") }
}

public struct VmMigration: Decodable, Equatable, Hashable, Sendable {
    public let name: String
    public let phase: String
    public let sourceNode: String
    public let targetNode: String
    /// Unix ms.
    public let startedAt: Int64
    public let completed: Bool
    public let failed: Bool

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        phase = try c.field(.phase, "")
        sourceNode = try c.field(.sourceNode, "")
        targetNode = try c.field(.targetNode, "")
        startedAt = try c.field(.startedAt, 0)
        completed = try c.field(.completed, false)
        failed = try c.field(.failed, false)
    }

    private enum CodingKeys: String, CodingKey { case name, phase, sourceNode, targetNode, startedAt, completed, failed }
}

/// One DataVolume a VM boots from, with its import, clone or upload progress.
public struct VmDisk: Decodable, Equatable, Hashable, Sendable {
    public let name: String
    public let phase: String
    /// "45.00%", "N/A" or "".
    public let progress: String
    public let importing: Bool

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        phase = try c.field(.phase, "")
        progress = try c.field(.progress, "")
        importing = try c.field(.importing, false)
    }

    private enum CodingKeys: String, CodingKey { case name, phase, progress, importing }
}

/// What KubeVMAction answers: the migration a migrate started, "" otherwise.
public struct VmActionResult: Decodable, Equatable, Sendable {
    public let action: String
    public let namespace: String
    public let name: String
    public let migration: String

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        action = try c.field(.action, "")
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        migration = try c.field(.migration, "")
    }

    private enum CodingKeys: String, CodingKey { case action, namespace, name, migration }
}

/// How the list groups a VM's status (its badge colour).
public enum VmState: String, Sendable, Hashable {
    case failing, changing, running, stopped
}

/// What a VM row says is in progress.
public enum VmProgress: Equatable, Sendable {
    case importing(disk: String, progress: String)
    case migrating(from: String, to: String)
}

/// The actions KubeVMAction runs; the raw value is its wire name.
public enum VmAction: String, CaseIterable, Identifiable, Sendable {
    case start, stop, restart, migrate, pause, unpause

    public var id: String { rawValue }

    /// The can-I check that gates it.
    public var kubeAction: KubeAction {
        switch self {
        case .start: .vmStart
        case .stop: .vmStop
        case .restart: .vmRestart
        case .migrate: .vmMigrate
        case .pause: .vmPause
        case .unpause: .vmUnpause
        }
    }
}
