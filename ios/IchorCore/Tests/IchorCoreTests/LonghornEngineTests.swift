import XCTest
@testable import IchorCore

final class LonghornEngineTests: XCTestCase {
    private let old = "longhornio/longhorn-engine:v1.12.0"
    private let new = "longhornio/longhorn-engine:v1.13.0"

    func testPreviewDecodesAndAcceptsOnlyADeployedOtherImage() throws {
        let raw = #"""
            {"namespace":"storage","name":"vol","state":"attached","robustness":"healthy",
             "currentImage":"longhornio/longhorn-engine:v1.12.0","specImage":"longhornio/longhorn-engine:v1.12.0",
             "defaultImage":"longhornio/longhorn-engine:v1.13.0","live":true,"canUpgrade":true,"reason":"",
             "images":[{"image":"longhornio/longhorn-engine:v1.13.0","version":"v1.13.0","deployed":true,"default":true,"incompatible":false},
                       {"image":"longhornio/longhorn-engine:v1.12.0","version":"v1.12.0","deployed":true,"default":false},
                       {"image":"example/engine:v1.14.0","deployed":false},
                       {"image":"example/engine:v1.0.0","deployed":true,"incompatible":true}],
             "error":""}
            """#
        let p = try TalosJSON.decode(LonghornEngineUpgradePreview.self, from: raw)
        XCTAssertTrue(p.live)
        XCTAssertEqual(p.images.count, 4)
        XCTAssertTrue(p.images[0].isDefault)
        XCTAssertEqual(p.images[2].version, "")
        XCTAssertEqual(p.preselected, new)
        XCTAssertTrue(p.accepts(new))
        XCTAssertFalse(p.accepts(""))
        XCTAssertFalse(p.accepts(old), "the running engine")
        XCTAssertFalse(p.accepts("example/engine:v1.14.0"), "not deployed")
        XCTAssertFalse(p.accepts("example/engine:v1.0.0"), "incompatible")
        XCTAssertFalse(p.accepts("foreign/engine:v9"), "not listed")
    }

    func testRefusedPreviewAcceptsNothing() throws {
        let p = try TalosJSON.decode(
            LonghornEngineUpgradePreview.self,
            from: #"""
                {"state":"attached","robustness":"degraded","currentImage":"a:v1","live":true,"canUpgrade":false,
                 "reason":"The volume is degraded: a live upgrade needs a healthy volume.",
                 "images":[{"image":"a:v2","deployed":true,"default":true}]}
                """#)
        XCTAssertEqual(p.reason, "The volume is degraded: a live upgrade needs a healthy volume.")
        XCTAssertEqual(p.preselected, "a:v2")
        XCTAssertFalse(p.accepts("a:v2"))
        let failed = LonghornEngineUpgradePreview(
            currentImage: "a:v1", canUpgrade: true, images: [LonghornEngineImage(image: "a:v2", deployed: true)],
            error: "boom")
        XCTAssertFalse(failed.accepts("a:v2"))
    }

    func testEmptyPreviewDecodesWithDefaults() throws {
        let p = try TalosJSON.decode(LonghornEngineUpgradePreview.self, from: "{}")
        XCTAssertEqual(p, LonghornEngineUpgradePreview())
        XCTAssertEqual(p.preselected, "")
        XCTAssertFalse(p.live)
    }

    func testPreselectIsTheDefaultOnly() {
        let p = LonghornEngineUpgradePreview(
            currentImage: "a:v1", canUpgrade: true,
            images: [
                LonghornEngineImage(image: "a:v3", deployed: false, isDefault: true),
                LonghornEngineImage(image: "a:v1", deployed: true),
                LonghornEngineImage(image: "a:v2", deployed: true),
            ])
        // The default is not deployed yet: nothing is preselected, the user picks one (as on Android).
        XCTAssertEqual(p.preselected, "")
    }

    func testVolumeEngineFieldsAndUpgradable() throws {
        func volume(_ extra: String) throws -> LonghornVolume {
            try TalosJSON.decode(LonghornVolume.self, from: #"{"name":"v","namespace":"longhorn-system",\#(extra)}"#)
        }
        let outdated = try volume(
            #""state":"attached","engineImage":"longhornio/longhorn-engine:v1.12.0","engineCurrentImage":"longhornio/longhorn-engine:v1.12.0","engineOutdated":true,"engineVersion":"v1.12.0""#
        )
        XCTAssertEqual(outdated.engineImage, old)
        XCTAssertEqual(outdated.engineCurrentImage, old)
        XCTAssertTrue(outdated.engineUpgradable)
        XCTAssertEqual(outdated.engineLabel, "v1.12.0")
        XCTAssertTrue(try volume(#""state":"detached","engineOutdated":true"#).engineUpgradable)
        XCTAssertFalse(try volume(#""state":"attaching","engineOutdated":true"#).engineUpgradable)
        XCTAssertFalse(try volume(#""state":"attached","engineOutdated":true,"engineUpgrading":true"#).engineUpgradable)
        XCTAssertFalse(try volume(#""state":"attached""#).engineUpgradable)
        // An older core sends none of them.
        let plain = try volume(#""state":"attached""#)
        XCTAssertEqual(plain.engineImage, "")
        XCTAssertFalse(plain.engineUpgrading)
        XCTAssertEqual(plain.engineLabel, "")
        // No version: the image tag.
        XCTAssertEqual(
            try volume(#""engineCurrentImage":"registry.example:5000/longhorn-engine:v1.11.2""#).engineLabel, "v1.11.2")
    }

    func testStatusEngineImages() throws {
        let s = try TalosJSON.decode(
            LonghornStatus.self,
            from: #"""
                {"version":"v1.13.0","defaultEngineImage":"longhornio/longhorn-engine:v1.13.0",
                 "volumes":[{"name":"a","engineOutdated":true},{"name":"b"},{"name":"c","engineOutdated":true}],
                 "engineImages":[{"image":"longhornio/longhorn-engine:v1.13.0","version":"v1.13.0","state":"deployed","refCount":1,"deployedNodes":3,"nodes":3},
                                 {"image":"longhornio/longhorn-engine:v1.12.0","version":"v1.12.0","state":"deployed","refCount":2,"incompatible":false}]}
                """#)
        XCTAssertEqual(s.engineImages.count, 2)
        XCTAssertEqual(s.engineImages[0].deployedNodes, 3)
        XCTAssertEqual(s.engineImages[1].refCount, 2)
        XCTAssertEqual(s.defaultEngineVersion, "v1.13.0")
        XCTAssertEqual(s.outdatedEngineVolumes, 2)

        let older = try TalosJSON.decode(LonghornStatus.self, from: "{}")
        XCTAssertEqual(older.defaultEngineImage, "")
        XCTAssertEqual(older.engineImages, [])
        XCTAssertEqual(older.defaultEngineVersion, "")
        XCTAssertEqual(older.outdatedEngineVolumes, 0)
    }

    func testImageTag() {
        XCTAssertEqual(longhornImageTag("longhornio/longhorn-engine:v1.12.0"), "v1.12.0")
        XCTAssertEqual(longhornImageTag("registry.example:5000/longhorn-engine"), "")
        XCTAssertEqual(longhornImageTag(""), "")
    }
}
