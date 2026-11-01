import Foundation

// Mirrors go/ichorgo/cloud_ovh.go.

/// A service account, sealed on the device; what the core gets per call.
public struct OVHCredential: Codable, Equatable, Sendable {
    public let endpoint: String
    public let clientId: String
    public let clientSecret: String
    public let project: String

    public init(endpoint: String, clientId: String, clientSecret: String, project: String) {
        self.endpoint = endpoint
        self.clientId = clientId
        self.clientSecret = clientSecret
        self.project = project
    }
}

/// What a service account sees and may do; `checked` is false when the right could not be told.
public struct OVHCheck: Decodable, Equatable, Sendable {
    public let project: String
    public let projects: [String]
    public let canCreate: Bool
    public let checked: Bool

    private enum CodingKeys: String, CodingKey { case project, projects, canCreate, checked }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        project = try c.field(.project, "")
        projects = try c.field(.projects, [])
        canCreate = try c.field(.canCreate, false)
        checked = try c.field(.checked, false)
    }
}

/// What an instance can be created from; prices before tax, in `currency`, empty without the catalog.
public struct OVHOptions: Decodable, Equatable, Sendable {
    public let project: String
    public let currency: String
    public let regions: [String]
    public let flavors: [OVHFlavor]
    public let images: [OVHImage]

    private enum CodingKeys: String, CodingKey { case project, currency, regions, flavors, images }

    // Go encodes empty (nil) slices as null.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        project = try c.field(.project, "")
        currency = try c.field(.currency, "")
        regions = try c.field(.regions, [])
        flavors = try c.field(.flavors, [])
        images = try c.field(.images, [])
    }

    /// The flavors of `region`, smallest first as the core lists them.
    public func flavors(in region: String) -> [OVHFlavor] { flavors.filter { $0.region == region } }

    /// The images of `region`, Talos ones first as the core lists them.
    public func images(in region: String) -> [OVHImage] { images.filter { $0.region == region } }
}

public struct OVHFlavor: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let id: String
    public let name: String
    public let region: String
    public let vcpus: Int
    public let ramGB: Int
    public let diskGB: Int
    public let hourly: String

    private enum CodingKeys: String, CodingKey { case id, name, region, vcpus, ramGB, diskGB, hourly }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        name = try c.field(.name, "")
        region = try c.field(.region, "")
        vcpus = try c.field(.vcpus, 0)
        ramGB = try c.field(.ramGB, 0)
        diskGB = try c.field(.diskGB, 0)
        hourly = try c.field(.hourly, "")
    }
}

public struct OVHImage: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let id: String
    public let name: String
    public let region: String
    public let talos: Bool

    private enum CodingKeys: String, CodingKey { case id, name, region, talos }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        name = try c.field(.name, "")
        region = try c.field(.region, "")
        talos = try c.field(.talos, false)
    }
}

/// StartOVHCreate's specJSON. Never holds the account.
public struct OVHCreateSpec: Encodable, Equatable, Sendable {
    public let region: String
    public let flavorId: String
    public let imageId: String
    public let hostname: String
    public let role: String
    public let templateNode: String
    public let installDisk: String

    public init(
        region: String, flavorId: String, imageId: String, hostname: String, role: String = "",
        templateNode: String = "", installDisk: String = ""
    ) {
        self.region = region
        self.flavorId = flavorId
        self.imageId = imageId
        self.hostname = hostname
        self.role = role
        self.templateNode = templateNode
        self.installDisk = installDisk
    }
}
