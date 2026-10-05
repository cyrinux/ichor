import XCTest
@testable import IchorCore

final class VpnOnlyTests: XCTestCase {
    func testRemovedClustersAreForgotten() {
        XCTAssertEqual(keepVpnOnly(saved: ["fp-prod", "fp-gone"], fingerprints: ["fp-prod", "fp-lab"]), ["fp-prod"])
    }

    func testBlankFingerprintsAreNeverKept() {
        XCTAssertEqual(keepVpnOnly(saved: [""], fingerprints: ["", "fp-lab"]), [])
    }

    func testVpnOnlyClusterIsHeldBackWithoutVpn() {
        XCTAssertTrue(heldBackForVpn(vpnOnly: ["fp-prod"], fingerprint: "fp-prod", vpnUp: false))
    }

    func testVpnOnlyClusterIsReachedWithVpn() {
        XCTAssertFalse(heldBackForVpn(vpnOnly: ["fp-prod"], fingerprint: "fp-prod", vpnUp: true))
    }

    func testOtherClustersAreNeverHeldBack() {
        XCTAssertFalse(heldBackForVpn(vpnOnly: ["fp-prod"], fingerprint: "fp-lab", vpnUp: false))
        XCTAssertFalse(heldBackForVpn(vpnOnly: [""], fingerprint: "", vpnUp: false))
        XCTAssertFalse(heldBackForVpn(vpnOnly: ["fp-prod"], fingerprint: nil, vpnUp: false))
    }

    func testTunnelInterfacesMeanAVpn() {
        XCTAssertTrue(vpnIsUp(interfaces: [PathInterface(name: "en0", isOther: false), PathInterface(name: "utun4", isOther: true)], usesOther: false))
        XCTAssertTrue(vpnIsUp(interfaces: [PathInterface(name: "ipsec0", isOther: true)], usesOther: false))
        XCTAssertTrue(vpnIsUp(interfaces: [], usesOther: true))
    }

    func testPlainNetworksAreNoVpn() {
        XCTAssertFalse(vpnIsUp(interfaces: [PathInterface(name: "en0", isOther: false), PathInterface(name: "pdp_ip0", isOther: false)], usesOther: false))
        // A tunnel name on a non-tunnel interface type, or another virtual interface, is not one.
        XCTAssertFalse(vpnIsUp(interfaces: [PathInterface(name: "utun0", isOther: false), PathInterface(name: "bridge100", isOther: true)], usesOther: false))
    }
}
