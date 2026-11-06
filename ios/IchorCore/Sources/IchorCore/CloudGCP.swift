import Foundation

// Mirrors go/ichorgo/cloud_gcp.go, cloud_gcp_options.go and cloud_gcp_create.go.

/// A service account key, its project and zone, sealed on the device; what the core gets per call.
public struct GCPCredential: Codable, Equatable, Sendable {
    public let serviceAccountJson: String
    public let project: String
    public let zone: String

    public init(serviceAccountJson: String, project: String = "", zone: String = "") {
        self.serviceAccountJson = serviceAccountJson
        self.project = project
        self.zone = zone
    }

    private enum CodingKeys: String, CodingKey { case serviceAccountJson, project, zone }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        serviceAccountJson = try c.field(.serviceAccountJson, "")
        project = try c.field(.project, "")
        zone = try c.field(.zone, "")
    }

    /// The same key in another project or zone (nil keeps the current one).
    public func with(project: String? = nil, zone: String? = nil) -> GCPCredential {
        GCPCredential(serviceAccountJson: serviceAccountJson, project: project ?? self.project, zone: zone ?? self.zone)
    }
}

/// The two fields of a pasted service account key the sheet shows; never the private key.
public struct GCPKeyIdentity: Equatable, Sendable {
    public let clientEmail: String
    public let projectId: String

    /// `text` read as a service account key (type service_account, with an email and a private
    /// key); nil when it is not one.
    public init?(key text: String) {
        guard let data = text.trimmingCharacters(in: .whitespacesAndNewlines).data(using: .utf8),
            let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
            object["type"] as? String == "service_account",
            let email = object["client_email"] as? String, !email.isEmpty,
            let privateKey = object["private_key"] as? String, !privateKey.isEmpty
        else { return nil }
        clientEmail = email
        projectId = object["project_id"] as? String ?? ""
    }
}

/// Whose key this is and what of the printed role it lacks; `checked` is false when that could not be told.
public struct GCPCheck: Decodable, Equatable, Sendable {
    public let account: String
    public let project: String
    public let canCreate: Bool
    public let checked: Bool
    public let missing: [String]

    private enum CodingKeys: String, CodingKey { case account, project, canCreate, checked, missing }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        account = try c.field(.account, "")
        project = try c.field(.project, "")
        canCreate = try c.field(.canCreate, false)
        checked = try c.field(.checked, false)
        missing = try c.field(.missing, [])
    }
}

/// What a VM can be created from in `zone` of `project`; `warnings` are the core's, shown as they are.
public struct GCPOptions: Decodable, Equatable, Sendable {
    public let project: String
    public let zone: String
    public let projects: [GCPProject]
    public let zones: [GCPZone]
    public let machineTypes: [GCPMachineType]
    public let networks: [GCPNetwork]
    public let images: [GCPImage]
    public let warnings: [String]

    private enum CodingKeys: String, CodingKey {
        case project, zone, projects, zones, machineTypes, networks, images, warnings
    }

    // Go encodes empty (nil) slices as null.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        project = try c.field(.project, "")
        zone = try c.field(.zone, "")
        projects = try c.field(.projects, [])
        zones = try c.field(.zones, [])
        machineTypes = try c.field(.machineTypes, [])
        networks = try c.field(.networks, [])
        images = try c.field(.images, [])
        warnings = try c.field(.warnings, [])
    }

    /// The architectures the machine types run, amd64 first.
    public var arches: [String] {
        var seen: [String] = []
        for type in machineTypes where !seen.contains(type.arch) { seen.append(type.arch) }
        return seen.sorted { ($0 == "amd64" ? "" : $0) < ($1 == "amd64" ? "" : $1) }
    }

    /// The machine types of `arch`, in the core's order (vCPUs, then memory).
    public func machineTypes(for arch: String) -> [GCPMachineType] { machineTypes.filter { $0.arch == arch } }

    /// The images `type` can boot: its arch, and UEFI on Arm; labelled ones first, as the core sorts them.
    public func images(for type: GCPMachineType) -> [GCPImage] {
        images.filter { $0.arch == type.arch && (type.arch != "arm64" || $0.uefi) }
    }

    /// The networks with a subnet in the zone's region, the only ones a VM there can use.
    public var usableNetworks: [GCPNetwork] { networks.filter { !$0.subnets.isEmpty } }

    /// The subnets of `network` in `region` (the core lists the zone's region only).
    public func subnets(in network: String, region: String) -> [GCPSubnet] {
        networks.first { $0.name == network }?.subnets.filter { region.isEmpty || $0.region == region } ?? []
    }

    /// The region of the picked zone.
    public var region: String {
        zones.first { $0.name == zone }?.region
            ?? (zone.lastIndex(of: "-").map { String(zone[..<$0]) } ?? zone)
    }

    /// The core's warnings about a route out without a public address, shown under the switch.
    public var natWarnings: [String] { warnings.filter { $0.contains("Cloud NAT") } }

    /// The other warnings, shown at the top.
    public var otherWarnings: [String] { warnings.filter { !$0.contains("Cloud NAT") } }
}

public struct GCPProject: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let id: String
    public let name: String

    private enum CodingKeys: String, CodingKey { case id, name }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        name = try c.field(.name, "")
    }

    /// "Example project (example-project)", or the id when the name is the same or empty.
    public var label: String { name.isEmpty || name == id ? id : "\(name) (\(id))" }
}

public struct GCPZone: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let name: String
    public let region: String
    public var id: String { name }

    private enum CodingKeys: String, CodingKey { case name, region }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        region = try c.field(.region, "")
    }
}

public struct GCPMachineType: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let name: String
    public let vcpus: Int
    public let memoryGB: Double
    /// amd64 or arm64.
    public let arch: String
    public var id: String { name }

    private enum CodingKeys: String, CodingKey { case name, vcpus, memoryGB, arch }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        vcpus = try c.field(.vcpus, 0)
        memoryGB = try c.field(.memoryGB, 0)
        arch = try c.field(.arch, "")
    }

    /// "4", or "0.5" for a fraction.
    public var memoryLabel: String {
        memoryGB.rounded() == memoryGB ? String(Int(memoryGB)) : String(memoryGB)
    }
}

public struct GCPNetwork: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let name: String
    public let selfLink: String
    public let autoSubnets: Bool
    /// Only the zone's region's.
    public let subnets: [GCPSubnet]
    public var id: String { name }

    private enum CodingKeys: String, CodingKey { case name, selfLink, autoSubnets, subnets }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        selfLink = try c.field(.selfLink, "")
        autoSubnets = try c.field(.autoSubnets, false)
        subnets = try c.field(.subnets, [])
    }
}

public struct GCPSubnet: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let name: String
    public let selfLink: String
    public let region: String
    public let cidr: String
    public var id: String { selfLink.isEmpty ? name : selfLink }

    private enum CodingKeys: String, CodingKey { case name, selfLink, region, cidr }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        selfLink = try c.field(.selfLink, "")
        region = try c.field(.region, "")
        cidr = try c.field(.cidr, "")
    }
}

public struct GCPImage: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let name: String
    public let selfLink: String
    public let arch: String
    public let sizeGB: Int64
    /// The ichor-talos-version label, "" for an image of your own without it.
    public let talosVersion: String
    public let schematic: String
    public let uefi: Bool
    public var id: String { selfLink.isEmpty ? name : selfLink }

    private enum CodingKeys: String, CodingKey { case name, selfLink, arch, sizeGB, talosVersion, schematic, uefi }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        selfLink = try c.field(.selfLink, "")
        arch = try c.field(.arch, "")
        sizeGB = try c.field(.sizeGB, Int64(0))
        talosVersion = try c.field(.talosVersion, "")
        schematic = try c.field(.schematic, "")
        uefi = try c.field(.uefi, false)
    }
}

/// The on-demand hourly price in USD before tax; `hourly` is empty when the Billing Catalog does not answer.
public struct GCPPrice: Decodable, Equatable, Sendable {
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

/// StartGCPCreate's specJSON. Never holds the key.
public struct GCPCreateSpec: Encodable, Equatable, Sendable {
    public let project: String
    public let zone: String
    public let machineType: String
    /// The image's selfLink (or its name in `project`).
    public let image: String
    /// The subnet's selfLink (or its name in the zone's region).
    public let subnetwork: String
    public let associatePublicIp: Bool
    public let diskGB: Int
    public let hostname: String
    public let role: String
    public let templateNode: String
    public let installDisk: String
    /// On Omni, the labels to sign into the join token (`omniLabelsJSON`); "" for none.
    public let labelsJson: String

    public static let defaultDiskGB = 20
    public static let diskRange = 10...65536

    public init(
        project: String, zone: String, machineType: String, image: String, subnetwork: String,
        associatePublicIp: Bool = true, diskGB: Int = GCPCreateSpec.defaultDiskGB, hostname: String,
        role: String = "", templateNode: String = "", installDisk: String = "", labelsJson: String = ""
    ) {
        self.project = project
        self.zone = zone
        self.machineType = machineType
        self.image = image
        self.subnetwork = subnetwork
        self.associatePublicIp = associatePublicIp
        self.diskGB = diskGB
        self.hostname = hostname
        self.role = role
        self.templateNode = templateNode
        self.installDisk = installDisk
        self.labelsJson = labelsJson
    }
}

public enum GCPNames {
    /// Compute Engine's instance name rule (RFC 1035), which the host name must follow: the VM is named after it.
    public static func valid(_ name: String) -> Bool {
        name.range(of: "^[a-z]([-a-z0-9]{0,61}[a-z0-9])?$", options: .regularExpression) != nil
    }

    /// The first https link of a core message: where to enable an API.
    public static func activationLink(in message: String) -> URL? {
        guard let range = message.range(of: #"https://\S+"#, options: .regularExpression) else { return nil }
        var link = String(message[range])
        while let last = link.last, ".,)".contains(last) { link.removeLast() }
        return URL(string: link)
    }
}
