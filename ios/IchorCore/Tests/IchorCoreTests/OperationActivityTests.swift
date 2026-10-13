import XCTest
@testable import IchorCore

final class OperationActivityTests: XCTestCase {
    func testAKindRoundTripsThroughItsLink() {
        for kind in OperationKind.allCases {
            XCTAssertEqual(OperationKind(link: kind.link), kind)
        }
        XCTAssertNil(OperationKind(link: URL(string: "ichor://open?c=abc&n=10.0.0.2")!))
        XCTAssertNil(OperationKind(link: URL(string: "ichor://open?op=reboot")!))
        XCTAssertNil(OperationKind(link: URL(string: "https://example.com/open?op=upgrade")!))
    }

    func testEachRunOffersItsButtons() {
        XCTAssertEqual(OperationButton.offered(for: .clusterUpgrade), [.pause, .abort])
        XCTAssertEqual(OperationButton.offered(for: .clusterUpgrade, paused: true), [.resume, .abort])
        XCTAssertEqual(OperationButton.offered(for: .configTry), [])
        XCTAssertEqual(OperationButton.offered(for: .configTry, waiting: true), [.keep, .revert])
        XCTAssertEqual(OperationButton.offered(for: .upgrade), [.stopFollowing])
        XCTAssertEqual(OperationButton.offered(for: .imagePull), [.stop])
        XCTAssertEqual(OperationButton.offered(for: .maintenance, stopping: true), [])
    }

    func testStopAbortAndKeepOpenTheAppWhenLocked() {
        XCTAssertEqual(OperationButton.allCases.filter(\.opensAppWhenLocked), [.stop, .abort, .keep])
    }

    func testABarOnlyWhereTheCoreCounts() {
        let pull = OperationContent(title: "Image pull", step: "1/3 nodes", done: 1, total: 3)
        XCTAssertTrue(pull.isDeterminate)
        XCTAssertEqual(pull.count, "1/3")
        XCTAssertEqual(pull.fraction, 1.0 / 3.0, accuracy: 0.0001)

        let upgrade = OperationContent(title: "Upgrading w-1", step: "rebooting")
        XCTAssertFalse(upgrade.isDeterminate)
        XCTAssertNil(upgrade.count)
        XCTAssertEqual(upgrade.fraction, 0)

        // Past the end stays on the bar.
        XCTAssertEqual(OperationContent(title: "", step: "", done: 7, total: 5).count, "5/5")
    }

    func testTheStepCountFollowsTheCore() {
        XCTAssertEqual(operationCount(index: 1, total: 5)?.done, 1)
        XCTAssertEqual(operationCount(index: 9, total: 5)?.done, 5)
        XCTAssertNil(operationCount(index: 0, total: 0))
    }

    func testAnEndedRunHasNoButtons() {
        let done = OperationContent(title: "Upgrading v1.11.0", step: "Every node runs v1.11.0", phase: .succeeded, buttons: [.pause, .abort])
        XCTAssertEqual(done.buttons, [])
        XCTAssertTrue(OperationPhase.stopped.ended)
        XCTAssertFalse(OperationPhase.paused.ended)
    }

    func testTheLockedContentNamesNothing() {
        let deadline = Date(timeIntervalSince1970: 1_000)
        let content = OperationContent(title: "Trying a config change on cp-1", step: "Reverts at 10:42", deadline: deadline, buttons: [.keep, .revert])
        let masked = content.masked(title: "Config try")
        XCTAssertEqual(masked.title, "Config try")
        XCTAssertEqual(masked.step, "")
        XCTAssertTrue(masked.locked)
        // The countdown and the buttons stay: they name nothing.
        XCTAssertEqual(masked.deadline, deadline)
        XCTAssertEqual(masked.buttons, [.keep, .revert])
    }
}
