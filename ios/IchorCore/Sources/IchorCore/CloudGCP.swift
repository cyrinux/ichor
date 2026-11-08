import Foundation

// Mirrors go/ichorgo/cloud_gcp.go, cloud_gcp_options.go and cloud_gcp_create.go.

/// A service account key, its project and zone, sealed on the device; what the core gets per call.
/// A Google-account sign-in has no key but a `googleSignIn` object (its session, refresh token
/// included) and the signed-in `account`: the app seals the JSON the core returns as it is and
/// never re-encodes it from this type (it would drop the sign-in; see `edited(_:…)`).
public struct GCPCredential: Codable, Equatable, Sendable {
    public let serviceAccountJson: String
    public let project: String
    public let zone: String
    /// The signed-in Google account's email, "" for a key.
    public var account = ""
    /// A Google-account sign-in (the JSON has `googleSignIn`), not a service account key.
    public private(set) var googleAccount = false

    public init(serviceAccountJson: String, project: String = "", zone: String = "") {
        self.serviceAccountJson = serviceAccountJson
        self.project = project
        self.zone = zone
    }

    private enum CodingKeys: String, CodingKey { case serviceAccountJson, project, zone, account, googleSignIn }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        serviceAccountJson = try c.field(.serviceAccountJson, "")
        project = try c.field(.project, "")
        zone = try c.field(.zone, "")
        account = try c.field(.account, "")
        googleAccount = c.contains(.googleSignIn) && (try? c.decodeNil(forKey: .googleSignIn)) == false
    }

    // A key only: a sign-in is never encoded from here.
    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(serviceAccountJson, forKey: .serviceAccountJson)
        try c.encode(project, forKey: .project)
        try c.encode(zone, forKey: .zone)
        if !account.isEmpty { try c.encode(account, forKey: .account) }
    }

    /// The same key in another project or zone (nil keeps the current one).
    public func with(project: String? = nil, zone: String? = nil) -> GCPCredential {
        GCPCredential(serviceAccountJson: serviceAccountJson, project: project ?? self.project, zone: zone ?? self.zone)
    }

    /// `json` with another project and zone (nil keeps the current one), every other field (a
    /// sign-in's `googleSignIn`) kept; nil when it is not a JSON object.
    public static func edited(_ json: String, project: String? = nil, zone: String? = nil) -> String? {
        guard var object = jsonObject(json) else { return nil }
        if let project { object["project"] = project }
        if let zone { object["zone"] = zone }
        return encoded(object)
    }

    /// A sign-in's credential (GCPSignInCredential's, no project yet) bound to `project`, the
    /// quota project the calls are billed to.
    public static func withProject(_ json: String, project: String) -> String? {
        edited(json, project: project)
    }

    /// `stored` with the `googleSignIn` of `updated` (a call's renewed credential), every other
    /// field of `stored` kept: a project or zone picked while the call ran stays. nil when either
    /// is not a JSON object or `updated` has no sign-in.
    public static func renewed(_ stored: String, from updated: String) -> String? {
        guard var current = jsonObject(stored),
            let signIn = jsonObject(updated)?["googleSignIn"] as? [String: Any]
        else { return nil }
        current["googleSignIn"] = signIn
        return encoded(current)
    }

    private static func jsonObject(_ json: String) -> [String: Any]? {
        (try? JSONSerialization.jsonObject(with: Data(json.utf8))) as? [String: Any]
    }

    private static func encoded(_ object: [String: Any]) -> String? {
        guard let data = try? JSONSerialization.data(withJSONObject: object, options: [.sortedKeys]) else { return nil }
        return String(decoding: data, as: UTF8.self)
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

/// Whose key or sign-in this is and what of the printed role it lacks; `checked` is false when
/// that could not be told. `credential` is a sign-in the core renewed, to seal again ("" when it
/// did not); `renewHint` says how to sign in again once it ends ("" for a key).
public struct GCPCheck: Decodable, Equatable, Sendable {
    public let account: String
    public let project: String
    public let canCreate: Bool
    public let checked: Bool
    public let missing: [String]
    public let credential: String
    public let renewHint: String

    private enum CodingKeys: String, CodingKey {
        case account, project, canCreate, checked, missing, credential, renewHint
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        account = try c.field(.account, "")
        project = try c.field(.project, "")
        canCreate = try c.field(.canCreate, false)
        checked = try c.field(.checked, false)
        missing = try c.field(.missing, [])
        credential = try c.field(.credential, "")
        renewHint = try c.field(.renewHint, "")
    }
}

/// GCPSignInCredential's answer: the sign-in (no project yet), the account, and the projects it
/// sees for the quota-project pick; `more` when the list was cut short.
public struct GCPSignInResult: Decodable, Equatable, Sendable {
    public let credential: String
    public let account: String
    public let projects: [GCPProject]
    public let more: Bool

    private enum CodingKeys: String, CodingKey { case credential, account, projects, more }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        credential = try c.field(.credential, "")
        account = try c.field(.account, "")
        projects = try c.field(.projects, [])
        more = try c.field(.more, false)
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
    /// A sign-in the core renewed, to seal again; "" when it did not.
    public let credential: String

    private enum CodingKeys: String, CodingKey {
        case project, zone, projects, zones, machineTypes, networks, images, warnings, credential
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
        credential = try c.field(.credential, "")
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

/// StartGCPImage's specJSON. The image's URL comes from the template, never from here.
public struct GCPImageSpec: Encodable, Equatable, Sendable {
    public let templateNode: String
    public let arch: String
    public let project: String
    public let zone: String
    /// The subnet's selfLink (or its name in the zone's region).
    public let subnetwork: String
    public let associatePublicIp: Bool
    /// "" for the core's default (e2-small or t2a-standard-1).
    public let machineType: String
    /// "" for talos-<version>-gcp-<arch>-<schematic12>.
    public let name: String

    public init(
        templateNode: String = "", arch: String, project: String, zone: String, subnetwork: String,
        associatePublicIp: Bool = true, machineType: String = "", name: String = ""
    ) {
        self.templateNode = templateNode
        self.arch = arch
        self.project = project
        self.zone = zone
        self.subnetwork = subnetwork
        self.associatePublicIp = associatePublicIp
        self.machineType = machineType
        self.name = name
    }
}

/// What a finished image run reports.
public struct GCPImageResult: Decodable, Equatable, Sendable {
    public let image: String
    public let selfLink: String
    public let arch: String
    public let talosVersion: String
    public let schematic: String
    public let sizeGB: Int

    /// Its id to pick it (as `GCPImage.id`) and its name to say so.
    public var made: MadeImage { MadeImage(id: selfLink.isEmpty ? image : selfLink, name: image) }

    private enum CodingKeys: String, CodingKey { case image, selfLink, arch, talosVersion, schematic, sizeGB }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        image = try c.field(.image, "")
        selfLink = try c.field(.selfLink, "")
        arch = try c.field(.arch, "")
        talosVersion = try c.field(.talosVersion, "")
        schematic = try c.field(.schematic, "")
        sizeGB = try c.field(.sizeGB, 0)
    }
}

/// The cost line of an image run: the helper's `machineType` and its `hourly` price, the data
/// disk's and the image's prices per GB-month, before tax in `currency`; a price the Billing
/// Catalog does not give is "".
public struct GCPImageEstimate: Decodable, Equatable, Sendable {
    public let machineType: String
    public let hourly: String
    public let currency: String
    public let diskMonthlyPerGB: String
    public let imageMonthlyPerGB: String
    public let minutes: Int

    private enum CodingKeys: String, CodingKey {
        case machineType, hourly, currency, diskMonthlyPerGB, imageMonthlyPerGB, minutes
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        machineType = try c.field(.machineType, "")
        hourly = try c.field(.hourly, "")
        currency = try c.field(.currency, "")
        diskMonthlyPerGB = try c.field(.diskMonthlyPerGB, "")
        imageMonthlyPerGB = try c.field(.imageMonthlyPerGB, "")
        minutes = try c.field(.minutes, 0)
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
