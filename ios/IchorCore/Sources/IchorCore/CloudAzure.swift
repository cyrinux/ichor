import Foundation

// Mirrors go/ichorgo/cloud_azure.go, cloud_azure_options.go and cloud_azure_create.go.

/// A service principal (az ad sp create-for-rbac's output plus the subscription), sealed on the
/// device; what the core gets per call. `resourceGroup` scopes Test's permission check and
/// `location` the options listed; both follow the person's picks. A Microsoft-account sign-in has
/// `kind` "user" and no secret: its refresh token stays in the JSON the core returns, which the app
/// seals as it is and never re-encodes from this type (it would drop it; see `edited(_:…)`).
public struct AzureCredential: Codable, Equatable, Sendable {
    public let tenantId: String
    public let clientId: String
    public let clientSecret: String
    public let subscriptionId: String
    /// "" or AzurePublicCloud, AzureChinaCloud, AzureUSGovernmentCloud.
    public let environment: String
    public let resourceGroup: String
    public let location: String
    public var kind = ""
    public var account = ""

    public init(
        tenantId: String, clientId: String, clientSecret: String, subscriptionId: String, environment: String = "",
        resourceGroup: String = "", location: String = ""
    ) {
        self.tenantId = tenantId
        self.clientId = clientId
        self.clientSecret = clientSecret
        self.subscriptionId = subscriptionId
        self.environment = environment
        self.resourceGroup = resourceGroup
        self.location = location
    }

    private enum CodingKeys: String, CodingKey {
        case tenantId, clientId, clientSecret, subscriptionId, environment, resourceGroup, location
        case kind, account
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        tenantId = try c.field(.tenantId, "")
        clientId = try c.field(.clientId, "")
        clientSecret = try c.field(.clientSecret, "")
        subscriptionId = try c.field(.subscriptionId, "")
        environment = try c.field(.environment, "")
        resourceGroup = try c.field(.resourceGroup, "")
        location = try c.field(.location, "")
        kind = try c.field(.kind, "")
        account = try c.field(.account, "")
    }

    /// A Microsoft-account sign-in, not a service principal.
    public var microsoftAccount: Bool { kind == "user" }

    /// `json` with another resource group (when given) and location, every other field (a
    /// sign-in's refresh token) kept; nil when it is not a JSON object.
    public static func edited(_ json: String, resourceGroup: String? = nil, location: String) -> String? {
        guard var object = (try? JSONSerialization.jsonObject(with: Data(json.utf8))) as? [String: Any] else {
            return nil
        }
        if let resourceGroup { object["resourceGroup"] = resourceGroup }
        object["location"] = location
        guard let data = try? JSONSerialization.data(withJSONObject: object, options: [.sortedKeys]) else { return nil }
        return String(decoding: data, as: UTF8.self)
    }

    /// `stored` with the refresh token of `updated` (a call's renewed credential), every other
    /// field of `stored` kept: a resource group or location picked while the call ran stays. nil
    /// when either is not a JSON object or `updated` has no token.
    public static func renewed(_ stored: String, from updated: String) -> String? {
        guard var current = jsonObject(stored), let token = jsonObject(updated)?["refreshToken"] as? String,
            !token.isEmpty
        else { return nil }
        current["refreshToken"] = token
        guard let data = try? JSONSerialization.data(withJSONObject: current, options: [.sortedKeys]) else {
            return nil
        }
        return String(decoding: data, as: UTF8.self)
    }

    private static func jsonObject(_ json: String) -> [String: Any]? {
        (try? JSONSerialization.jsonObject(with: Data(json.utf8))) as? [String: Any]
    }

    /// The same principal for another resource group and location.
    public func with(resourceGroup: String? = nil, location: String) -> AzureCredential {
        AzureCredential(
            tenantId: tenantId, clientId: clientId, clientSecret: clientSecret, subscriptionId: subscriptionId,
            environment: environment, resourceGroup: resourceGroup ?? self.resourceGroup, location: location)
    }
}

/// Whose principal or account this is and whether it may create VMs; `checked` is false when that
/// could not be told. `account` is the signed-in Microsoft account, "" for a principal; `credential`
/// is the credential JSON to seal again when the core rotated its refresh token.
public struct AzureCheck: Decodable, Equatable, Sendable {
    public let tenant: String
    public let subscription: String
    public let subscriptionName: String
    public let canCreate: Bool
    public let checked: Bool
    public let account: String
    public let credential: String

    private enum CodingKeys: String, CodingKey {
        case tenant, subscription, subscriptionName, canCreate, checked, account, credential
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        tenant = try c.field(.tenant, "")
        subscription = try c.field(.subscription, "")
        subscriptionName = try c.field(.subscriptionName, "")
        canCreate = try c.field(.canCreate, false)
        checked = try c.field(.checked, false)
        account = try c.field(.account, "")
        credential = try c.field(.credential, "")
    }
}

/// What a VM can be created from in `location`; `warnings` are the core's, shown as they are.
public struct AzureOptions: Decodable, Equatable, Sendable {
    public let subscription: String
    public let location: String
    public let subscriptions: [AzureSubscription]
    public let locations: [AzureLocation]
    public let resourceGroups: [AzureResourceGroup]
    public let sizes: [AzureSize]
    public let networks: [AzureNetwork]
    public let securityGroups: [AzureSecurityGroup]
    public let images: [AzureImage]
    public let warnings: [String]
    /// The credential JSON to seal again when the call rotated its refresh token, else "".
    public let credential: String

    private enum CodingKeys: String, CodingKey {
        case subscription, location, subscriptions, locations, resourceGroups, sizes, networks, securityGroups, images
        case warnings, credential
    }

    // Go encodes empty (nil) slices as null.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        subscription = try c.field(.subscription, "")
        location = try c.field(.location, "")
        subscriptions = try c.field(.subscriptions, [])
        locations = try c.field(.locations, [])
        resourceGroups = try c.field(.resourceGroups, [])
        sizes = try c.field(.sizes, [])
        networks = try c.field(.networks, [])
        securityGroups = try c.field(.securityGroups, [])
        images = try c.field(.images, [])
        warnings = try c.field(.warnings, [])
        credential = try c.field(.credential, "")
    }

    /// The VM sizes that boot `image` (its architecture and Hyper-V generation), in the core's order.
    public func sizes(for image: AzureImage) -> [AzureSize] {
        sizes.filter { $0.arch == image.arch && $0.generations.contains(image.generation) }
    }

    /// The subnets of the virtual network `network`; empty for an unknown one.
    public func subnets(in network: String) -> [AzureSubnet] {
        networks.first { $0.id == network }?.subnets ?? []
    }

    /// The resource group's location, or the listed one when the group is unknown.
    public func location(of resourceGroup: String) -> String {
        resourceGroups.first { $0.name == resourceGroup }?.location.lowercased() ?? location
    }

    /// The display name of `name` ("West Europe"), or the name itself.
    public func locationLabel(_ name: String) -> String {
        guard let display = locations.first(where: { $0.name == name })?.displayName, !display.isEmpty else {
            return name
        }
        return display
    }
}

public struct AzureSubscription: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let id: String
    public let name: String

    private enum CodingKeys: String, CodingKey { case id, name }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        name = try c.field(.name, "")
    }
}

public struct AzureLocation: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let name: String
    public let displayName: String
    public var id: String { name }

    private enum CodingKeys: String, CodingKey { case name, displayName }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        displayName = try c.field(.displayName, "")
    }
}

public struct AzureResourceGroup: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let name: String
    public let location: String
    public var id: String { name }

    private enum CodingKeys: String, CodingKey { case name, location }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        location = try c.field(.location, "")
    }
}

public struct AzureSize: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let name: String
    public let vcpus: Int
    public let memoryGB: Double
    /// amd64 or arm64.
    public let arch: String
    /// The Hyper-V generations it boots: V1, V2.
    public let generations: [String]
    public var id: String { name }

    private enum CodingKeys: String, CodingKey { case name, vcpus, memoryGB, arch, generations }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        vcpus = try c.field(.vcpus, 0)
        memoryGB = try c.field(.memoryGB, 0)
        arch = try c.field(.arch, "")
        generations = try c.field(.generations, [])
    }

    /// "Standard_B2s · 2 CPU · 4 GB".
    public var label: String {
        let memory = memoryGB.rounded() == memoryGB ? String(Int(memoryGB)) : String(memoryGB)
        return "\(name) · \(vcpus) CPU · \(memory) GB"
    }
}

public struct AzureNetwork: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let id: String
    public let name: String
    public let resourceGroup: String
    public let location: String
    public let subnets: [AzureSubnet]

    private enum CodingKeys: String, CodingKey { case id, name, resourceGroup, location, subnets }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        name = try c.field(.name, "")
        resourceGroup = try c.field(.resourceGroup, "")
        location = try c.field(.location, "")
        subnets = try c.field(.subnets, [])
    }
}

public struct AzureSubnet: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let id: String
    public let name: String
    public let cidr: String
    /// The subnet's own security group, empty when it has none.
    public let nsgId: String
    /// It reaches the Internet without a public IP on the VM.
    public let natGateway: Bool

    private enum CodingKeys: String, CodingKey { case id, name, cidr, nsgId, natGateway }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        name = try c.field(.name, "")
        cidr = try c.field(.cidr, "")
        nsgId = try c.field(.nsgId, "")
        natGateway = try c.field(.natGateway, false)
    }
}

public struct AzureSecurityGroup: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let id: String
    public let name: String
    public let resourceGroup: String
    /// The Internet reaches port 50000 (the Talos API).
    public let wideOpen: Bool

    private enum CodingKeys: String, CodingKey { case id, name, resourceGroup, wideOpen }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        name = try c.field(.name, "")
        resourceGroup = try c.field(.resourceGroup, "")
        wideOpen = try c.field(.wideOpen, false)
    }
}

public struct AzureImage: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let id: String
    public let name: String
    public let resourceGroup: String
    /// managed (an image) or gallery (a gallery image version).
    public let source: String
    public let arch: String
    /// V1 or V2.
    public let generation: String
    public let talosVersion: String
    public let schematic: String

    private enum CodingKeys: String, CodingKey {
        case id, name, resourceGroup, source, arch, generation, talosVersion, schematic
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        name = try c.field(.name, "")
        resourceGroup = try c.field(.resourceGroup, "")
        source = try c.field(.source, "")
        arch = try c.field(.arch, "")
        generation = try c.field(.generation, "")
        talosVersion = try c.field(.talosVersion, "")
        schematic = try c.field(.schematic, "")
    }
}

/// The pay-as-you-go hourly price before tax; `hourly` is empty when the Retail Prices API does not answer.
public struct AzurePrice: Decodable, Equatable, Sendable {
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

/// StartAzureCreate's specJSON. Never holds the principal.
public struct AzureCreateSpec: Encodable, Equatable, Sendable {
    public let resourceGroup: String
    public let location: String
    public let size: String
    public let imageId: String
    public let subnetId: String
    public let securityGroupId: String
    public let associatePublicIp: Bool
    public let osDiskGB: Int
    public let hostname: String
    public let role: String
    public let templateNode: String
    public let installDisk: String
    /// On Omni, the labels to sign into the join token (`omniLabelsJSON`); "" for none.
    public let labelsJson: String

    public init(
        resourceGroup: String, location: String, size: String, imageId: String, subnetId: String,
        securityGroupId: String = "", associatePublicIp: Bool = false, osDiskGB: Int = 0, hostname: String,
        role: String = "", templateNode: String = "", installDisk: String = "", labelsJson: String = ""
    ) {
        self.resourceGroup = resourceGroup
        self.location = location
        self.size = size
        self.imageId = imageId
        self.subnetId = subnetId
        self.securityGroupId = securityGroupId
        self.associatePublicIp = associatePublicIp
        self.osDiskGB = osDiskGB
        self.hostname = hostname
        self.role = role
        self.templateNode = templateNode
        self.installDisk = installDisk
        self.labelsJson = labelsJson
    }
}

/// StartAzureImage's specJSON. The image's URL comes from the template, never from here.
public struct AzureImageSpec: Encodable, Equatable, Sendable {
    public let templateNode: String
    public let arch: String
    public let resourceGroup: String
    public let location: String
    public let subnetId: String
    /// "" for none: the subnet's own group applies, if it has one.
    public let securityGroupId: String
    /// "" for the core's default (Standard_B2s or Standard_B2pls_v2).
    public let size: String
    /// "" for talos-<version>-azure-<arch>-<schematic12>.
    public let name: String

    public init(
        templateNode: String = "", arch: String, resourceGroup: String, location: String, subnetId: String,
        securityGroupId: String = "", size: String = "", name: String = ""
    ) {
        self.templateNode = templateNode
        self.arch = arch
        self.resourceGroup = resourceGroup
        self.location = location
        self.subnetId = subnetId
        self.securityGroupId = securityGroupId
        self.size = size
        self.name = name
    }
}

/// What a finished image run reports.
public struct AzureImageResult: Decodable, Equatable, Sendable {
    public let imageId: String
    public let name: String
    public let arch: String
    public let talosVersion: String
    public let schematic: String
    /// V1 (amd64) or V2 (arm64).
    public let generation: String
    public let sizeGB: Int

    /// Its id to pick it and its name to say so.
    public var made: MadeImage { MadeImage(id: imageId, name: name) }

    private enum CodingKeys: String, CodingKey { case imageId, name, arch, talosVersion, schematic, generation, sizeGB }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        imageId = try c.field(.imageId, "")
        name = try c.field(.name, "")
        arch = try c.field(.arch, "")
        talosVersion = try c.field(.talosVersion, "")
        schematic = try c.field(.schematic, "")
        generation = try c.field(.generation, "")
        sizeGB = try c.field(.sizeGB, 0)
    }
}

/// The cost line of an image run: the helper's `size` and its `hourly` price before tax in
/// `currency` ("" when the Retail Prices API does not give it), for about `minutes`.
public struct AzureImageEstimate: Decodable, Equatable, Sendable {
    public let size: String
    public let hourly: String
    public let currency: String
    public let imageNote: String
    public let minutes: Int

    private enum CodingKeys: String, CodingKey { case size, hourly, currency, imageNote, minutes }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        size = try c.field(.size, "")
        hourly = try c.field(.hourly, "")
        currency = try c.field(.currency, "")
        imageNote = try c.field(.imageNote, "")
        minutes = try c.field(.minutes, 0)
    }
}

/// The OS disk sizes: the default, the smallest a Talos image fits in, and Azure's largest.
public enum AzureDisk {
    public static let defaultGB = 20
    public static let minGB = 10
    public static let maxGB = 4095

    /// A typed OS disk size in GB, or nil when it is not a size Azure takes.
    public static func gigabytes(_ text: String) -> Int? {
        guard let value = Int(text.trimmingCharacters(in: .whitespaces)), (minGB...maxGB).contains(value) else {
            return nil
        }
        return value
    }
}

/// AzureSignInStart: the code to enter at `verificationUri`, and the credential for the next step.
public struct AzureSignInStart: Decodable, Equatable, Sendable {
    public let verificationUri: String
    public let userCode: String
    public let expiresIn: Int
    public let credential: String

    private enum CodingKeys: String, CodingKey { case verificationUri, userCode, expiresIn, credential }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        verificationUri = try c.field(.verificationUri, "")
        userCode = try c.field(.userCode, "")
        expiresIn = try c.field(.expiresIn, 0)
        credential = try c.field(.credential, "")
    }
}

/// One subscription the signed-in account sees.
public struct AzureSubscriptionChoice: Decodable, Equatable, Hashable, Sendable {
    public let id: String
    public let name: String
    public let tenantId: String

    private enum CodingKeys: String, CodingKey { case id, name, tenantId }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        name = try c.field(.name, "")
        tenantId = try c.field(.tenantId, "")
    }

    public var label: String { name.isEmpty ? id : name }
}

/// AzureSignInFinish: who signed in, and the subscriptions to pick from.
public struct AzureSignInFinish: Decodable, Equatable, Sendable {
    public let credential: String
    public let account: String
    public let subscriptions: [AzureSubscriptionChoice]

    private enum CodingKeys: String, CodingKey { case credential, account, subscriptions }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        credential = try c.field(.credential, "")
        account = try c.field(.account, "")
        subscriptions = try c.field(.subscriptions, [])
    }
}

/// AzureSignInChoose: the credential bound to the picked subscription.
public struct AzureSignInChoice: Decodable, Equatable, Sendable {
    public let credential: String
    public let account: String
    public let subscriptionName: String

    private enum CodingKeys: String, CodingKey { case credential, account, subscriptionName }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        credential = try c.field(.credential, "")
        account = try c.field(.account, "")
        subscriptionName = try c.field(.subscriptionName, "")
    }
}
