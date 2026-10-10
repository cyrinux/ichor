import XCTest
@testable import IchorCore

final class NodeFeaturesTests: XCTestCase {
    private func features(_ json: String) throws -> NodeFeatures {
        try TalosJSON.decode(NodeFeatures.self, from: json)
    }

    func testDecoding() throws {
        let decoded = try features("""
        {"version":"v1.7.6","features":{
          "events":{"supported":true,"minVersion":"","reason":""},
          "volumes":{"supported":false,"minVersion":"v1.8","reason":"needs Talos v1.8 or newer"},
          "diskHealth":{"supported":false}}}
        """)
        XCTAssertEqual(decoded.version, "v1.7.6")
        XCTAssertEqual(decoded.features["volumes"], FeatureSupport(supported: false, minVersion: "v1.8", reason: "needs Talos v1.8 or newer"))
        XCTAssertEqual(decoded.features["diskHealth"], FeatureSupport(supported: false))
        XCTAssertEqual(try features("{}"), NodeFeatures())
        XCTAssertEqual(try features(#"{"version":"v1.9.0","features":null}"#).features, [:])
    }

    func testEveryNameIsAGoName() {
        XCTAssertEqual(NodeFeature.allCases.count, 28)
        XCTAssertEqual(NodeFeature.logFollow.rawValue, "logFollow")
        XCTAssertEqual(NodeFeature.etcdMemberActions.rawValue, "etcdMemberActions")
    }

    func testSupportedWhileUnknown() throws {
        XCTAssertTrue(featureSupport(nil, .volumes).supported)
        let old = try features(#"{"version":"v1.7.6","features":{"volumes":{"supported":false,"minVersion":"1.8"}}}"#)
        XCTAssertFalse(featureSupport(old, .volumes).supported)
        // A name this core does not report.
        XCTAssertTrue(featureSupport(old, .diskHealth).supported)
        XCTAssertNil(featureSupport(old, .diskHealth).notice)
        XCTAssertEqual(featureSupport(old, .volumes).notice, VersionNotice(minVersion: "v1.8"))
    }

    func testNotice() {
        XCTAssertNil(FeatureSupport(supported: true, minVersion: "v1.8").notice)
        // With a reason, the version is the one it names…
        XCTAssertEqual(FeatureSupport(supported: false, minVersion: "v1.8", reason: "needs Talos v1.9 or newer").notice,
                       VersionNotice(minVersion: "v1.9"))
        // …or none: "removed in" must not read as "needs".
        XCTAssertEqual(FeatureSupport(supported: false, minVersion: "v1.2",
                                      reason: "not available on this node's Talos version (v1.18.0): removed in Talos v1.18").notice,
                       VersionNotice(minVersion: ""))
        XCTAssertEqual(FeatureSupport(supported: false, reason: "no SMART on this platform").notice, VersionNotice(minVersion: ""))
        // Only without a reason does minVersion speak.
        XCTAssertEqual(FeatureSupport(supported: false, minVersion: "1.8").notice, VersionNotice(minVersion: "v1.8"))
    }

    func testVersionNoticeOfErrors() {
        XCTAssertEqual(versionNotice("volumes: needs Talos v1.8 or newer"), VersionNotice(minVersion: "v1.8"))
        XCTAssertEqual(versionNotice("Needs talos 1.10.2 or newer (node runs v1.9.1)"), VersionNotice(minVersion: "v1.10.2"))
        // Go's volumes reason: the version it needs wins over the one the node runs.
        XCTAssertEqual(versionNotice("not available on this node's Talos version (v1.7.6): volumes need Talos v1.8 or newer"),
                       VersionNotice(minVersion: "v1.8"))
        XCTAssertEqual(versionNotice("not available on this node's Talos version (v1.7.6)"), VersionNotice(minVersion: ""))
        XCTAssertNil(versionNotice("you need to upgrade"))
        XCTAssertEqual(versionNotice("Unsupported: no such API"), VersionNotice(minVersion: ""))
        XCTAssertEqual(versionNotice("Unimplemented: unknown method DiskUsage"), VersionNotice(minVersion: ""))
        XCTAssertEqual(versionNotice("disk health is not available on this node's Talos version"), VersionNotice(minVersion: ""))
        XCTAssertEqual(versionNotice("Not available on this Talos version."), VersionNotice(minVersion: ""))
        XCTAssertNil(versionNotice("permission denied (talosconfig role too limited): no"))
        XCTAssertNil(versionNotice("unreachable: connection refused"))
        XCTAssertNil(versionNotice("needs Talos or newer"))
        XCTAssertNil(versionNotice("needs Talos v1 or newer"))
        XCTAssertNil(versionNotice("needs Talos v1.8"))
        XCTAssertNil(versionNotice(""))
    }

    func testVersions() {
        XCTAssertEqual(displayTalosVersion("1.15"), "v1.15")
        XCTAssertEqual(displayTalosVersion(" v1.9.0 "), "v1.9.0")
        XCTAssertEqual(displayTalosVersion("V1.9"), "v1.9")
        XCTAssertEqual(displayTalosVersion("  "), "")
        XCTAssertLessThan(compareTalosVersions("v1.9", "1.15.2"), 0)
        XCTAssertGreaterThan(compareTalosVersions("v1.10.0", "v1.9.9"), 0)
        XCTAssertEqual(compareTalosVersions("v1.8", "1.8.0"), 0)
        XCTAssertLessThan(compareTalosVersions("", "v0.1"), 0)
    }

    func testClusterSupport() throws {
        let new = try features(#"{"features":{"etcdMemberActions":{"supported":true}}}"#)
        let old = try features(#"{"features":{"etcdMemberActions":{"supported":false,"minVersion":"v1.10"}}}"#)
        let older = try features(#"{"features":{"etcdMemberActions":{"supported":false,"minVersion":"v1.9"}}}"#)
        let vague = try features(#"{"features":{"etcdMemberActions":{"supported":false,"reason":"no"}}}"#)
        XCTAssertTrue(clusterSupport([], .etcdMemberActions).supported)
        XCTAssertTrue(clusterSupport([NodeFeatures()], .etcdMemberActions).supported)
        // One node is enough.
        XCTAssertTrue(clusterSupport([old, new], .etcdMemberActions).supported)
        // Unsupported everywhere: the lowest version that would bring it.
        XCTAssertEqual(clusterSupport([old, older, vague], .etcdMemberActions), FeatureSupport(supported: false, minVersion: "v1.9"))
        XCTAssertEqual(clusterSupport([vague], .etcdMemberActions), FeatureSupport(supported: false, reason: "no"))
    }
}
