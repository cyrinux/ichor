import Foundation

// Mirrors go/ichorgo/cloud_aws.go, cloud_aws_options.go and cloud_aws_create.go.

/// Access keys and their region, sealed on the device; what the core gets per call.
public struct AWSCredential: Codable, Equatable, Sendable {
    public let accessKeyId: String
    public let secretAccessKey: String
    public let sessionToken: String
    public let region: String

    public init(accessKeyId: String, secretAccessKey: String, sessionToken: String = "", region: String) {
        self.accessKeyId = accessKeyId
        self.secretAccessKey = secretAccessKey
        self.sessionToken = sessionToken
        self.region = region
    }

    private enum CodingKeys: String, CodingKey { case accessKeyId, secretAccessKey, sessionToken, region }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        accessKeyId = try c.field(.accessKeyId, "")
        secretAccessKey = try c.field(.secretAccessKey, "")
        sessionToken = try c.field(.sessionToken, "")
        region = try c.field(.region, "")
    }

    /// The same keys in another region.
    public func with(region: String) -> AWSCredential {
        AWSCredential(
            accessKeyId: accessKeyId, secretAccessKey: secretAccessKey, sessionToken: sessionToken, region: region)
    }
}

/// Whose keys these are and whether they may run instances; `checked` is false when that could not be told.
public struct AWSCheck: Decodable, Equatable, Sendable {
    public let account: String
    public let arn: String
    public let region: String
    public let canCreate: Bool
    public let checked: Bool

    private enum CodingKeys: String, CodingKey { case account, arn, region, canCreate, checked }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        account = try c.field(.account, "")
        arn = try c.field(.arn, "")
        region = try c.field(.region, "")
        canCreate = try c.field(.canCreate, false)
        checked = try c.field(.checked, false)
    }
}

/// What an instance can be created from in `region`; `warnings` are the core's, shown as they are.
/// `templateVersion` and `templateSchematic` are the cluster's image, "" when they cannot be read.
public struct AWSOptions: Decodable, Equatable, Sendable {
    public let region: String
    public let regions: [String]
    public let types: [AWSInstanceType]
    public let subnets: [AWSSubnet]
    public let securityGroups: [AWSSecurityGroup]
    public let images: [AWSImage]
    public let templateVersion: String
    public let templateSchematic: String
    public let warnings: [String]

    private enum CodingKeys: String, CodingKey {
        case region, regions, types, subnets, securityGroups, images, templateVersion, templateSchematic, warnings
    }

    // Go encodes empty (nil) slices as null.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        region = try c.field(.region, "")
        regions = try c.field(.regions, [])
        types = try c.field(.types, [])
        subnets = try c.field(.subnets, [])
        securityGroups = try c.field(.securityGroups, [])
        images = try c.field(.images, [])
        templateVersion = try c.field(.templateVersion, "")
        templateSchematic = try c.field(.templateSchematic, "")
        warnings = try c.field(.warnings, [])
    }

    /// The architectures the instance types run, amd64 first.
    public var arches: [String] {
        var seen: [String] = []
        for type in types where !seen.contains(type.arch) { seen.append(type.arch) }
        return seen.sorted { ($0 == "amd64" ? "" : $0) < ($1 == "amd64" ? "" : $1) }
    }

    /// The instance types of `arch`, in the core's order.
    public func types(for arch: String) -> [AWSInstanceType] { types.filter { $0.arch == arch } }

    /// The images `arch` can boot: the cluster's image first, then official ones, then the account's own by name.
    public func images(for arch: String) -> [AWSImage] {
        images.filter { $0.arch == arch }.sorted {
            let (first, second) = ($0.matches(self), $1.matches(self))
            if first != second { return first }
            if $0.official != $1.official { return $0.official }
            return $0.name.lowercased() < $1.name.lowercased()
        }
    }

    /// Whether `arch` has an AMI of the cluster's image. When the cluster's image cannot be read,
    /// any AMI of the account's own counts, as nothing can be compared.
    public func hasClusterImage(for arch: String) -> Bool {
        let candidates = images(for: arch)
        if templateVersion.isEmpty || templateSchematic.isEmpty { return candidates.contains { !$0.official } }
        return candidates.contains { $0.matches(self) }
    }

    /// The security groups of `vpc`, the subnet's VPC: EC2 refuses the others.
    public func securityGroups(in vpc: String) -> [AWSSecurityGroup] { securityGroups.filter { $0.vpcId == vpc } }
}

public struct AWSInstanceType: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let name: String
    public let vcpus: Int
    public let memoryGB: Double
    /// amd64 or arm64.
    public let arch: String
    public let burstable: Bool
    public var id: String { name }

    private enum CodingKeys: String, CodingKey { case name, vcpus, memoryGB, arch, burstable }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        vcpus = try c.field(.vcpus, 0)
        memoryGB = try c.field(.memoryGB, 0)
        arch = try c.field(.arch, "")
        burstable = try c.field(.burstable, false)
    }

    /// "2", or "0.5" for a fraction.
    public var memoryLabel: String {
        memoryGB.rounded() == memoryGB ? String(Int(memoryGB)) : String(memoryGB)
    }
}

public struct AWSSubnet: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let id: String
    public let vpcId: String
    public let vpcDefault: Bool
    public let cidr: String
    public let az: String
    /// Assigns a public IPv4 address on launch.
    public let `public`: Bool
    public let name: String

    private enum CodingKeys: String, CodingKey { case id, vpcId, vpcDefault, cidr, az, `public`, name }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        vpcId = try c.field(.vpcId, "")
        vpcDefault = try c.field(.vpcDefault, false)
        cidr = try c.field(.cidr, "")
        az = try c.field(.az, "")
        `public` = try c.field(.public, false)
        name = try c.field(.name, "")
    }
}

public struct AWSSecurityGroup: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let id: String
    public let vpcId: String
    public let name: String
    public let description: String

    private enum CodingKeys: String, CodingKey { case id, vpcId, name, description }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        vpcId = try c.field(.vpcId, "")
        name = try c.field(.name, "")
        description = try c.field(.description, "")
    }
}

public struct AWSImage: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let id: String
    public let arch: String
    /// official (Sidero Labs' AMI of the cluster's version) or custom (the account's own).
    public let source: String
    public let name: String
    public let talosVersion: String
    public let schematic: String
    public var official: Bool { source == "official" }

    /// The cluster's image: same version, same schematic (an official AMI carries the default one).
    public func matches(_ options: AWSOptions) -> Bool {
        !options.templateVersion.isEmpty && !options.templateSchematic.isEmpty
            && talosVersion == options.templateVersion && schematic == options.templateSchematic
    }

    private enum CodingKeys: String, CodingKey { case id, arch, source, name, talosVersion, schematic }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        arch = try c.field(.arch, "")
        source = try c.field(.source, "")
        name = try c.field(.name, "")
        talosVersion = try c.field(.talosVersion, "")
        schematic = try c.field(.schematic, "")
    }
}

/// The on-demand hourly price before tax; `hourly` is empty when the Price List does not answer.
public struct AWSPrice: Decodable, Equatable, Sendable {
    public let hourly: String
    public let currency: String

    public init(hourly: String = "", currency: String = "") {
        self.hourly = hourly
        self.currency = currency
    }

    private enum CodingKeys: String, CodingKey { case hourly, currency }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        hourly = try c.field(.hourly, "")
        currency = try c.field(.currency, "")
    }
}

/// StartAWSImage's specJSON. The image's URL comes from the template, never from here.
public struct AWSImageSpec: Encodable, Equatable, Sendable {
    public let templateNode: String
    public let arch: String
    public let region: String
    public let subnetId: String
    /// "" for the VPC's default group.
    public let securityGroupId: String
    /// "" for the core's default (t3.small or t4g.small).
    public let instanceType: String
    /// "" for talos-<version>-aws-<arch>-<schematic12>.
    public let name: String

    public init(
        templateNode: String = "", arch: String, region: String, subnetId: String, securityGroupId: String = "",
        instanceType: String = "", name: String = ""
    ) {
        self.templateNode = templateNode
        self.arch = arch
        self.region = region
        self.subnetId = subnetId
        self.securityGroupId = securityGroupId
        self.instanceType = instanceType
        self.name = name
    }
}

/// What a finished AMI run reports.
public struct AWSImageResult: Decodable, Equatable, Sendable {
    public let imageId: String
    public let snapshotId: String
    public let name: String
    public let arch: String
    public let talosVersion: String
    public let schematic: String
    public let sizeGB: Int

    private enum CodingKeys: String, CodingKey { case imageId, snapshotId, name, arch, talosVersion, schematic, sizeGB }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        imageId = try c.field(.imageId, "")
        snapshotId = try c.field(.snapshotId, "")
        name = try c.field(.name, "")
        arch = try c.field(.arch, "")
        talosVersion = try c.field(.talosVersion, "")
        schematic = try c.field(.schematic, "")
        sizeGB = try c.field(.sizeGB, 0)
    }
}

/// The cost line of an AMI run; prices before tax, in `currency`, "" when the Price List does not
/// give them.
public struct AWSImageCost: Decodable, Equatable, Sendable {
    public let instanceType: String
    public let hourly: String
    public let currency: String
    public let snapshotMonthlyPerGB: String
    public let minutes: Int

    /// Both prices are known.
    public var priced: Bool { !hourly.isEmpty && !snapshotMonthlyPerGB.isEmpty }

    private enum CodingKeys: String, CodingKey { case instanceType, hourly, currency, snapshotMonthlyPerGB, minutes }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        instanceType = try c.field(.instanceType, "")
        hourly = try c.field(.hourly, "")
        currency = try c.field(.currency, "")
        snapshotMonthlyPerGB = try c.field(.snapshotMonthlyPerGB, "")
        minutes = try c.field(.minutes, 0)
    }
}

extension AWSOptions {
    /// The subnets a helper instance can reach the Image Factory from: those assigning a public IP first.
    public var helperSubnets: [AWSSubnet] {
        subnets.filter(\.public) + subnets.filter { !$0.public }
    }
}

/// StartAWSCreate's specJSON. Never holds the keys.
public struct AWSCreateSpec: Encodable, Equatable, Sendable {
    public let region: String
    public let instanceType: String
    public let imageId: String
    public let subnetId: String
    public let securityGroupIds: [String]
    public let associatePublicIp: Bool
    public let keyName: String
    public let hostname: String
    public let role: String
    public let templateNode: String
    public let installDisk: String
    /// On Omni, the labels to sign into the join token (`omniLabelsJSON`); "" for none.
    public let labelsJson: String

    public init(
        region: String, instanceType: String, imageId: String, subnetId: String, securityGroupIds: [String] = [],
        associatePublicIp: Bool = false, keyName: String = "", hostname: String, role: String = "",
        templateNode: String = "", installDisk: String = "", labelsJson: String = ""
    ) {
        self.region = region
        self.instanceType = instanceType
        self.imageId = imageId
        self.subnetId = subnetId
        self.securityGroupIds = securityGroupIds
        self.associatePublicIp = associatePublicIp
        self.keyName = keyName
        self.hostname = hostname
        self.role = role
        self.templateNode = templateNode
        self.installDisk = installDisk
        self.labelsJson = labelsJson
    }
}
