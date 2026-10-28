import XCTest
@testable import IchorCore

final class BootMediaTests: XCTestCase {
    // Invented: no real cluster's schematic.
    private let schematic = "9d2c5e4b1a7f0836c4d2e1b9a0f7c6d5e4b3a2918f7e6d5c4b3a29180f7e6d5c"

    func testPlainTalosMediaDecode() throws {
        let image = "https://factory.talos.dev/image/\(schematic)/v1.14.0"
        let media = try TalosJSON.decode(BootMedia.self, from: """
        {"talosVersion":"v1.14.0","schematic":"\(schematic)","schematicSource":"member","arch":"amd64","secureBoot":false,
         "templateNode":"192.0.2.10","iso":"\(image)/metal-amd64.iso",
         "ipxe":"https://pxe.factory.talos.dev/pxe/\(schematic)/v1.14.0/metal-amd64",
         "kernel":"\(image)/kernel-amd64","initramfs":"\(image)/initramfs-amd64.xz","cmdline":"\(image)/cmdline-metal-amd64",
         "booterDocker":"sudo docker run --rm --network host ghcr.io/siderolabs/booter:v0.4.0 --talos-version=v1.14.0 --schematic-id=\(schematic)",
         "booterPodman":"sudo podman run --rm --network host ghcr.io/siderolabs/booter:v0.4.0 --talos-version=v1.14.0 --schematic-id=\(schematic)",
         "omni":null,"warnings":[]}
        """)
        XCTAssertTrue(media.fromMember)
        XCTAssertEqual(media.arch, "amd64")
        XCTAssertTrue(media.iso.hasSuffix("/metal-amd64.iso"))
        XCTAssertNil(media.omni)
        XCTAssertEqual(media.booterValues.map(\.label), ["docker", "podman"])
        XCTAssertTrue(media.booterValues[1].text.hasPrefix("sudo podman "))
        XCTAssertEqual(media.pxeFileValues.count, 3)
    }

    func testOmniMediaWithNullsDecode() throws {
        let media = try TalosJSON.decode(BootMedia.self, from: """
        {"talosVersion":"v1.14.0","schematic":"\(schematic)","schematicSource":"default","arch":"arm64","secureBoot":true,
         "templateNode":"cp-1","omni":{"kernelArgs":"siderolink.api=https://omni.example.com?jointoken=sample",
         "booterDocker":"sudo docker run …","booterPodman":"sudo podman run …"},"warnings":null}
        """)
        XCTAssertFalse(media.fromMember)
        XCTAssertTrue(media.secureBoot)
        XCTAssertTrue(media.warnings.isEmpty)
        XCTAssertEqual(media.omni?.values.last?.text, "siderolink.api=https://omni.example.com?jointoken=sample")
    }
}
