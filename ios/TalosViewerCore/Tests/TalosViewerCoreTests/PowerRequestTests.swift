import XCTest
@testable import TalosViewerCore

final class PowerRequestTests: XCTestCase {
    func testCliModesMatchTalosctl() {
        XCTAssertEqual(RebootMode.allCases.map(\.cli), ["default", "powercycle", "force"])
    }

    func testTitles() {
        XCTAssertEqual(PowerRequest(action: .reboot).title, "Reboot")
        XCTAssertEqual(PowerRequest(action: .reboot, rebootMode: .powercycle).title, "Power cycle")
        XCTAssertEqual(PowerRequest(action: .reboot, rebootMode: .force).title, "Force reboot")
        XCTAssertEqual(PowerRequest(action: .shutdown).title, "Shut down")
        XCTAssertEqual(PowerRequest(action: .shutdown, forceShutdown: true).title, "Force shut down")
    }

    func testForcedOnlyForMatchingAction() {
        XCTAssertTrue(PowerRequest(action: .reboot, rebootMode: .force).forced)
        XCTAssertFalse(PowerRequest(action: .reboot, forceShutdown: true).forced)
        XCTAssertFalse(PowerRequest(action: .shutdown, rebootMode: .force).forced)
    }
}
