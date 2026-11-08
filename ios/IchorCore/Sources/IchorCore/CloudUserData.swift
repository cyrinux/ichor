import Foundation

// Mirrors go/ichorgo/boot_userdata.go.

/// The user data of a new node on a cloud host (Hetzner, OpenStack, Scaleway): pasted when the
/// server is created, read by Talos at first boot. `userData` is a credential: Omni's join config
/// (its join token) or a new member's full config with the cluster's secrets.
public struct CloudUserData: Decodable, Equatable, Sendable {
    /// "omni": Omni's join config; "talos": a new member's config made from `templateNode`'s.
    public let kind: String
    public let userData: String
    public let size: Int
    /// The 32 KiB most clouds accept as user data; `size` above it comes with a warning.
    public let limit: Int
    /// `userData` with its secrets hidden: what the screen shows.
    public let redactedPreview: String
    public let role: String
    public let hostname: String
    public let templateNode: String
    public let talosVersion: String
    public let schematic: String
    public let images: [CloudImage]
    public let warnings: [String]

    public var isOmni: Bool { kind == "omni" }
    public var overLimit: Bool { size > limit }

    private enum CodingKeys: String, CodingKey {
        case kind, userData, size, limit, redactedPreview, role, hostname, templateNode, talosVersion, schematic,
            images, warnings
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.field(.kind, "")
        userData = try c.field(.userData, "")
        size = try c.field(.size, 0)
        limit = try c.field(.limit, 0)
        redactedPreview = try c.field(.redactedPreview, "")
        role = try c.field(.role, "")
        hostname = try c.field(.hostname, "")
        templateNode = try c.field(.templateNode, "")
        talosVersion = try c.field(.talosVersion, "")
        schematic = try c.field(.schematic, "")
        images = try c.field(.images, [])
        warnings = try c.field(.warnings, [])
    }

    /// The images as boot card values, one tab per provider.
    public var imageValues: [BootValue] {
        images.map { BootValue(label: $0.providerLabel, text: $0.url) }
    }
}

/// The Image Factory disk image to create the server (or its snapshot) from on `platform`.
public struct CloudImage: Decodable, Equatable, Sendable {
    public let platform: String
    public let arch: String
    public let url: String

    private enum CodingKeys: String, CodingKey { case platform, arch, url }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        platform = try c.field(.platform, "")
        arch = try c.field(.arch, "")
        url = try c.field(.url, "")
    }

    /// The provider the image is for, as people know it.
    public var providerLabel: String {
        switch platform {
        case "hcloud": "Hetzner"
        case "openstack": "OpenStack"
        case "scaleway": "Scaleway"
        default: platform
        }
    }
}
