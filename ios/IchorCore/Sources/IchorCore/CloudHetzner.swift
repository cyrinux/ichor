import Foundation

// Mirrors go/ichorgo/cloud_hetzner.go.

/// What a Hetzner token may do.
public struct HetznerTokenCheck: Decodable, Equatable, Sendable {
    public let readOnly: Bool
    public let servers: Int

    private enum CodingKeys: String, CodingKey { case readOnly, servers }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        readOnly = try c.field(.readOnly, false)
        servers = try c.field(.servers, 0)
    }
}

/// What a server can be created from; prices before tax, in `currency`.
public struct HetznerOptions: Decodable, Equatable, Sendable {
    public let currency: String
    public let serverTypes: [HetznerServerType]
    public let locations: [HetznerLocation]
    public let snapshots: [HetznerSnapshot]

    private enum CodingKeys: String, CodingKey { case currency, serverTypes, locations, snapshots }

    // Go encodes empty (nil) slices as null.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        currency = try c.field(.currency, "")
        serverTypes = try c.field(.serverTypes, [])
        locations = try c.field(.locations, [])
        snapshots = try c.field(.snapshots, [])
    }

    /// The snapshots a server of `type` can start from, Talos ones first as the core lists them.
    public func snapshots(for type: HetznerServerType?) -> [HetznerSnapshot] {
        guard let type else { return [] }
        return snapshots.filter { $0.arch == type.arch }
    }
}

public struct HetznerServerType: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let name: String
    public let cores: Int
    public let memoryGB: Double
    public let diskGB: Double
    public let arch: String
    public let prices: [HetznerPrice]

    public var id: String { name }

    private enum CodingKeys: String, CodingKey { case name, cores, memoryGB, diskGB, arch, prices }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        cores = try c.field(.cores, 0)
        memoryGB = try c.field(.memoryGB, 0)
        diskGB = try c.field(.diskGB, 0)
        arch = try c.field(.arch, "")
        prices = try c.field(.prices, [])
    }

    public func price(in location: String) -> HetznerPrice? {
        prices.first { $0.location == location }
    }
}

public struct HetznerPrice: Decodable, Equatable, Hashable, Sendable {
    public let location: String
    public let hourly: String
    public let monthly: String

    private enum CodingKeys: String, CodingKey { case location, hourly, monthly }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        location = try c.field(.location, "")
        hourly = try c.field(.hourly, "")
        monthly = try c.field(.monthly, "")
    }
}

public struct HetznerLocation: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let name: String
    public let city: String

    public var id: String { name }

    private enum CodingKeys: String, CodingKey { case name, city }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        city = try c.field(.city, "")
    }
}

public struct HetznerSnapshot: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let id: Int64
    public let description: String
    public let arch: String
    public let talos: Bool

    private enum CodingKeys: String, CodingKey { case id, description, arch, talos }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, 0)
        description = try c.field(.description, "")
        arch = try c.field(.arch, "")
        talos = try c.field(.talos, false)
    }
}

extension HetznerOptions {
    /// The locations a throwaway server of `arch` can be made in.
    public func locations(forArch arch: String) -> [HetznerLocation] {
        locations.filter { location in
            serverTypes.contains { $0.arch == arch && $0.price(in: location.name) != nil }
        }
    }
}

/// StartHetznerSnapshot's specJSON. The image's URL comes from the template, never from here.
public struct HetznerSnapshotSpec: Encodable, Equatable, Sendable {
    public let templateNode: String
    public let arch: String
    public let location: String

    public init(templateNode: String = "", arch: String, location: String) {
        self.templateNode = templateNode
        self.arch = arch
        self.location = location
    }
}

/// What a finished snapshot run reports.
public struct HetznerSnapshotResult: Decodable, Equatable, Sendable {
    public let imageId: Int64
    public let description: String
    public let sizeGB: Double
    public let created: String

    private enum CodingKeys: String, CodingKey { case imageId, description, sizeGB, created }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        imageId = try c.field(.imageId, 0)
        description = try c.field(.description, "")
        sizeGB = try c.field(.sizeGB, 0)
        created = try c.field(.created, "")
    }
}

/// The cost line of a snapshot run; prices before tax, in `currency`.
public struct HetznerSnapshotCost: Decodable, Equatable, Sendable {
    public let serverType: String
    public let hourly: String
    public let monthlyPerGB: String
    public let currency: String

    private enum CodingKeys: String, CodingKey { case serverType, hourly, monthlyPerGB, currency }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        serverType = try c.field(.serverType, "")
        hourly = try c.field(.hourly, "")
        monthlyPerGB = try c.field(.monthlyPerGB, "")
        currency = try c.field(.currency, "")
    }
}

/// StartHetznerCreate's specJSON. Never holds the token.
public struct HetznerCreateSpec: Encodable, Equatable, Sendable {
    public let serverType: String
    public let location: String
    public let snapshotId: Int64
    public let hostname: String
    public let role: String
    public let templateNode: String
    public let installDisk: String

    public init(
        serverType: String, location: String, snapshotId: Int64, hostname: String, role: String = "",
        templateNode: String = "", installDisk: String = ""
    ) {
        self.serverType = serverType
        self.location = location
        self.snapshotId = snapshotId
        self.hostname = hostname
        self.role = role
        self.templateNode = templateNode
        self.installDisk = installDisk
    }
}
