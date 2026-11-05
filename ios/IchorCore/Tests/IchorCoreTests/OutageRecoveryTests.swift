import XCTest

@testable import IchorCore

// The same cases as app/src/test/java/name/levis/ichor/model/OutageRecoveryTest.kt.
final class OutageRecoveryTests: XCTestCase {
    private let expired = "authentication failed (certificate expired or wrong CA?)"
    private let hostnameMismatch = "x509: certificate is valid for lab-a, not lab-b"

    private func recovery(
        _ cause: OutageCause, omni: Bool = false, demo: Bool = false, masked: Bool = false, error: String? = nil
    ) -> OutageRecovery {
        let outage = ClusterOutage(cause: cause, nodes: 3, errors: [error ?? expired])
        return outageRecovery(outage, omni: omni, demo: demo, masked: masked)
    }

    private func network(scan: Bool = true, editEndpoints: Bool = true) -> OutageRecovery {
        OutageRecovery(
            primary: .vpnSettings, retrySecondary: true, scan: scan, editEndpoints: editEndpoints, explanation: nil)
    }

    func testANetworkOutageOffersWhatItAlwaysDid() {
        XCTAssertEqual(recovery(.network), network())
        XCTAssertEqual(recovery(.network, omni: true), network())
        XCTAssertEqual(recovery(.network, demo: true), network(scan: false, editEndpoints: false))
        XCTAssertEqual(recovery(.network, masked: true), network(editEndpoints: false))
    }

    func testARefusedDirectTalosconfigOffersToImportTheRenewedOne() {
        XCTAssertEqual(
            recovery(.credentials),
            OutageRecovery(
                primary: .replaceConfig, retrySecondary: true, scan: false, editEndpoints: false,
                explanation: .replaceConfig))
    }

    func testARefusedOmniContextOffersToSignInAgain() {
        XCTAssertEqual(
            recovery(.credentials, omni: true),
            OutageRecovery(
                primary: .signIn, retrySecondary: true, scan: false, editEndpoints: false, explanation: .omniSession))
    }

    func testACertificateForAnotherNameIsAnAddressProblemSoEndpointsStayEditable() {
        XCTAssertTrue(recovery(.credentials, error: hostnameMismatch).editEndpoints)
        XCTAssertTrue(recovery(.credentials, omni: true, error: hostnameMismatch).editEndpoints)
        XCTAssertFalse(recovery(.credentials, masked: true, error: hostnameMismatch).editEndpoints)
    }

    func testTheDemoHasNothingToSignInToOrReplace() {
        let expected = OutageRecovery(
            primary: .retry, retrySecondary: false, scan: false, editEndpoints: false, explanation: nil)
        XCTAssertEqual(recovery(.credentials, demo: true), expected)
        XCTAssertEqual(recovery(.credentials, omni: true, demo: true), expected)
        XCTAssertEqual(recovery(.credentials, demo: true, error: hostnameMismatch), expected)
    }

    func testAnyOtherFailureKeepsTheGenericPath() {
        let expected = OutageRecovery(
            primary: .retry, retrySecondary: false, scan: false, editEndpoints: true, explanation: nil)
        let locked = OutageRecovery(
            primary: .retry, retrySecondary: false, scan: false, editEndpoints: false, explanation: nil)
        XCTAssertEqual(recovery(.other), expected)
        XCTAssertEqual(recovery(.other, omni: true), expected)
        XCTAssertEqual(recovery(.other, demo: true), locked)
        XCTAssertEqual(recovery(.other, masked: true), locked)
    }
}
