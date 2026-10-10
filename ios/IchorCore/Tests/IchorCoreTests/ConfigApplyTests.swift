import XCTest
@testable import IchorCore

final class ConfigApplyTests: XCTestCase {
    func testModesFollowTheNeedForAReboot() {
        XCTAssertEqual(ConfigPreview(changed: true, lines: [], needsReboot: false).applyModes, [.auto, .staged, .reboot])
        XCTAssertEqual(ConfigPreview(changed: true, lines: [], needsReboot: true).applyModes, [.staged, .reboot])
        XCTAssertEqual(ConfigApplyMode.allCases.map(\.rawValue), ["auto", "staged", "reboot"])
    }

    func testProgressDecoding() throws {
        let p = try TalosJSON.decode(ConfigApplyProgress.self, from: #"{"phase":"rebooting","message":"the node reboots","at":5}"#)
        XCTAssertEqual(p.applyPhase, .rebooting)
        XCTAssertEqual(p.message, "the node reboots")
        XCTAssertEqual(try TalosJSON.decode(ConfigApplyProgress.self, from: #"{"phase":"later"}"#).applyPhase, .applying)
    }
}
