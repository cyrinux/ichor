import Foundation

/// An engine image a volume can move to, as the upgrade preview lists it.
public struct LonghornEngineImage: Decodable, Equatable, Identifiable, Sendable {
    public let image: String
    public let version: String
    /// Deployed on the volume's node and on every replica's node.
    public let deployed: Bool
    /// Longhorn's default engine image.
    public let isDefault: Bool
    public let incompatible: Bool

    public var id: String { image }

    /// It can be picked: deployed where the volume runs and compatible.
    public var selectable: Bool { deployed && !incompatible }

    public init(image: String, version: String = "", deployed: Bool, isDefault: Bool = false, incompatible: Bool = false) {
        self.image = image
        self.version = version
        self.deployed = deployed
        self.isDefault = isDefault
        self.incompatible = incompatible
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        image = try c.field(.image, "")
        version = try c.field(.version, "")
        deployed = try c.field(.deployed, false)
        isDefault = try c.field(.isDefault, false)
        incompatible = try c.field(.incompatible, false)
    }

    private enum CodingKeys: String, CodingKey {
        case image, version, deployed, incompatible
        case isDefault = "default"
    }
}

/// What the Go core says before a Longhorn volume's engine upgrade (KubeLonghornEngineUpgradePreview).
public struct LonghornEngineUpgradePreview: Decodable, Equatable, Sendable {
    public let namespace: String
    public let name: String
    public let state: String
    public let robustness: String
    public let currentImage: String
    public let specImage: String
    public let defaultImage: String
    /// Attached: the upgrade runs live, else offline.
    public let live: Bool
    public let canUpgrade: Bool
    /// Why it cannot, one English sentence from the core, shown as is.
    public let reason: String
    public let images: [LonghornEngineImage]
    public let error: String

    public init(namespace: String = "", name: String = "", state: String = "", robustness: String = "",
                currentImage: String = "", specImage: String = "", defaultImage: String = "", live: Bool = false,
                canUpgrade: Bool = false, reason: String = "", images: [LonghornEngineImage] = [], error: String = "") {
        self.namespace = namespace
        self.name = name
        self.state = state
        self.robustness = robustness
        self.currentImage = currentImage
        self.specImage = specImage
        self.defaultImage = defaultImage
        self.live = live
        self.canUpgrade = canUpgrade
        self.reason = reason
        self.images = images
        self.error = error
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        state = try c.field(.state, "")
        robustness = try c.field(.robustness, "")
        currentImage = try c.field(.currentImage, "")
        specImage = try c.field(.specImage, "")
        defaultImage = try c.field(.defaultImage, "")
        live = try c.field(.live, false)
        canUpgrade = try c.field(.canUpgrade, false)
        reason = try c.field(.reason, "")
        images = try c.field(.images, [])
        error = try c.field(.error, "")
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, state, robustness, currentImage, specImage, defaultImage, live, canUpgrade, reason, images, error
    }

    /// The image the sheet starts on: the default one when it can be picked, else the first that can,
    /// never the one already running; "" when none.
    public var preselected: String {
        images.first { $0.isDefault && $0.selectable && $0.image != currentImage }?.image ?? ""
    }

    /// Confirming moves the volume to `selected`: the core allows it, and the image is a listed,
    /// deployed, compatible one other than the running engine.
    public func accepts(_ selected: String) -> Bool {
        guard !selected.isEmpty, error.isEmpty, canUpgrade, selected != currentImage else { return false }
        return images.contains { $0.image == selected && $0.selectable }
    }
}
