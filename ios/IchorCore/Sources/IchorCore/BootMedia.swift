import Foundation

// Mirrors go/ichorgo/boot_media.go.

/// What boots a new machine of the cluster into Talos (step 0 of adding a node): the template
/// member's version and Image Factory schematic, the factory's links, and the siderolabs/booter
/// command that serves a network boot from a laptop of the machine's network.
public struct BootMedia: Decodable, Equatable, Sendable {
    public let talosVersion: String
    public let schematic: String
    /// "member": the template's own schematic; "default": the factory's, without extensions.
    public let schematicSource: String
    public let arch: String
    public let secureBoot: Bool
    public let templateNode: String
    public let iso: String
    public let ipxe: String
    public let kernel: String
    public let initramfs: String
    public let cmdline: String
    public let booterDocker: String
    public let booterPodman: String
    /// nil on a plain Talos cluster, and on Omni in privacy mode (see `warnings`).
    public let omni: BootMediaOmni?
    public let warnings: [String]

    public var fromMember: Bool { schematicSource == "member" }

    private enum CodingKeys: String, CodingKey {
        case talosVersion, schematic, schematicSource, arch, secureBoot, templateNode, iso, ipxe, kernel, initramfs,
             cmdline, booterDocker, booterPodman, omni, warnings
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        talosVersion = try c.field(.talosVersion, "")
        schematic = try c.field(.schematic, "")
        schematicSource = try c.field(.schematicSource, "")
        arch = try c.field(.arch, "")
        secureBoot = try c.field(.secureBoot, false)
        templateNode = try c.field(.templateNode, "")
        iso = try c.field(.iso, "")
        ipxe = try c.field(.ipxe, "")
        kernel = try c.field(.kernel, "")
        initramfs = try c.field(.initramfs, "")
        cmdline = try c.field(.cmdline, "")
        booterDocker = try c.field(.booterDocker, "")
        booterPodman = try c.field(.booterPodman, "")
        omni = try c.decodeIfPresent(BootMediaOmni.self, forKey: .omni)
        warnings = try c.field(.warnings, [])
    }
}

/// How a new machine joins an Omni instance. `kernelArgs` carries the join token: a secret.
public struct BootMediaOmni: Decodable, Equatable, Sendable {
    public let kernelArgs: String
    public let booterDocker: String
    public let booterPodman: String

    private enum CodingKeys: String, CodingKey { case kernelArgs, booterDocker, booterPodman }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kernelArgs = try c.field(.kernelArgs, "")
        booterDocker = try c.field(.booterDocker, "")
        booterPodman = try c.field(.booterPodman, "")
    }
}

/// The architectures the Image Factory builds media for.
public let bootArchitectures = ["amd64", "arm64"]

/// One value of a boot card, under a tab label when the card holds several (docker, podman).
public struct BootValue: Equatable, Sendable, Identifiable {
    public let label: String
    public let text: String
    public var id: String { label }

    public init(label: String, text: String) {
        self.label = label
        self.text = text
    }
}

extension BootMedia {
    /// The booter command for each container runtime.
    public var booterValues: [BootValue] {
        [BootValue(label: "docker", text: booterDocker), BootValue(label: "podman", text: booterPodman)]
    }

    /// The files of a PXE server of one's own.
    public var pxeFileValues: [BootValue] {
        [BootValue(label: "kernel", text: kernel), BootValue(label: "initramfs", text: initramfs),
         BootValue(label: "cmdline", text: cmdline)]
    }
}

extension BootMediaOmni {
    /// The booter commands with the join arguments, then the arguments alone.
    public var values: [BootValue] {
        [BootValue(label: "docker", text: booterDocker), BootValue(label: "podman", text: booterPodman),
         BootValue(label: "kernel args", text: kernelArgs)]
    }
}
