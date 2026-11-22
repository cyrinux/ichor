import Foundation

// Mirrors go/ichorgo/cloud_scaleway.go and cloud_scaleway_create.go.

/// An API key, sealed on the device; what the core gets per call. `zone` "" is fr-par-1.
public struct ScalewayCredential: Codable, Equatable, Sendable {
    public let accessKey: String
    public let secretKey: String
    public let project: String
    public let zone: String

    public init(accessKey: String, secretKey: String, project: String, zone: String = "") {
        self.accessKey = accessKey
        self.secretKey = secretKey
        self.project = project
        self.zone = zone
    }

    /// The same key in another zone: options are read per zone.
    public func inZone(_ zone: String) -> ScalewayCredential {
        ScalewayCredential(accessKey: accessKey, secretKey: secretKey, project: project, zone: zone)
    }

    /// A stored credential JSON moved to `zone`; unchanged when `zone` is empty or the JSON does not read.
    public static func json(_ stored: String, inZone zone: String) -> String {
        guard !zone.isEmpty, !stored.isEmpty,
            let key = try? JSONDecoder().decode(ScalewayCredential.self, from: Data(stored.utf8)),
            let data = try? JSONEncoder().encode(key.inZone(zone))
        else { return stored }
        return String(decoding: data, as: UTF8.self)
    }
}

/// What an API key may do; `checked` is false when the create right could not be told. `account` is
/// the access key id.
public struct ScalewayCheck: Decodable, Equatable, Sendable {
    public let project: String
    public let zone: String
    public let canCreate: Bool
    public let checked: Bool
    public let account: String

    private enum CodingKeys: String, CodingKey { case project, zone, canCreate, checked, account }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        project = try c.field(.project, "")
        zone = try c.field(.zone, "")
        canCreate = try c.field(.canCreate, false)
        checked = try c.field(.checked, false)
        account = try c.field(.account, "")
    }
}

/// What an Instance can be created from in `zone`; prices before tax. `warnings` are the core's,
/// worded in English.
public struct ScalewayOptions: Decodable, Equatable, Sendable {
    public let project: String
    public let zone: String
    public let zones: [String]
    public let types: [ScalewayType]
    public let images: [ScalewayImage]
    public let securityGroups: [ScalewaySecurityGroup]
    public let warnings: [String]

    private enum CodingKeys: String, CodingKey { case project, zone, zones, types, images, securityGroups, warnings }

    // Go encodes empty (nil) slices as null.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        project = try c.field(.project, "")
        zone = try c.field(.zone, "")
        zones = try c.field(.zones, [])
        types = try c.field(.types, [])
        images = try c.field(.images, [])
        securityGroups = try c.field(.securityGroups, [])
        warnings = try c.field(.warnings, [])
    }

    /// The images, Talos ones first (in the core's order otherwise).
    public var imagesTalosFirst: [ScalewayImage] { images.filter(\.talos) + images.filter { !$0.talos } }

    /// The types `image` can boot: those of its architecture; all of them before an image is picked.
    public func types(for image: ScalewayImage?) -> [ScalewayType] {
        guard let image, !image.arch.isEmpty else { return types }
        return types.filter { $0.arch == image.arch }
    }

    /// The project's default security group, else the first one; nil when the zone lists none.
    public var defaultSecurityGroup: ScalewaySecurityGroup? {
        securityGroups.first(where: \.isDefault) ?? securityGroups.first
    }
}

/// A server type; `blockOnly` when it has no local storage (the data volume is Block Storage).
public struct ScalewayType: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let name: String
    public let arch: String
    public let vcpus: Int
    public let ramGB: Int
    public let localDiskGB: Int
    public let hourly: String
    public let currency: String
    public let blockOnly: Bool

    public var id: String { name }

    private enum CodingKeys: String, CodingKey {
        case name, arch, vcpus, ramGB, localDiskGB, hourly, currency, blockOnly
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        arch = try c.field(.arch, "")
        vcpus = try c.field(.vcpus, 0)
        ramGB = try c.field(.ramGB, 0)
        localDiskGB = try c.field(.localDiskGB, 0)
        hourly = try c.field(.hourly, "")
        currency = try c.field(.currency, "")
        blockOnly = try c.field(.blockOnly, false)
    }

    /// "0.0099 EUR": the hourly price before tax; nil when the core has none.
    public var hourlyPrice: String? {
        guard !hourly.isEmpty else { return nil }
        return currency.isEmpty ? hourly : "\(hourly) \(currency)"
    }
}

public struct ScalewayImage: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let id: String
    public let name: String
    public let arch: String
    public let sizeGB: Int
    public let talos: Bool

    private enum CodingKeys: String, CodingKey { case id, name, arch, sizeGB, talos }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        name = try c.field(.name, "")
        arch = try c.field(.arch, "")
        sizeGB = try c.field(.sizeGB, 0)
        talos = try c.field(.talos, false)
    }
}

public struct ScalewaySecurityGroup: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let id: String
    public let name: String
    public let isDefault: Bool

    private enum CodingKeys: String, CodingKey {
        case id, name
        case isDefault = "default"
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        name = try c.field(.name, "")
        isDefault = try c.field(.isDefault, false)
    }
}

/// StartScalewayCreate's specJSON. Never holds the key.
public struct ScalewayCreateSpec: Encodable, Equatable, Sendable {
    public let zone: String
    public let type: String
    public let imageId: String
    /// "" for the project's default security group.
    public let securityGroupId: String
    public let hostname: String
    public let role: String
    public let templateNode: String
    public let installDisk: String
    /// On Omni, the labels to sign into the join token (`omniLabelsJSON`); "" for none.
    public let labelsJson: String

    public init(
        zone: String, type: String, imageId: String, securityGroupId: String = "", hostname: String,
        role: String = "", templateNode: String = "", installDisk: String = "", labelsJson: String = ""
    ) {
        self.zone = zone
        self.type = type
        self.imageId = imageId
        self.securityGroupId = securityGroupId
        self.hostname = hostname
        self.role = role
        self.templateNode = templateNode
        self.installDisk = installDisk
        self.labelsJson = labelsJson
    }
}
