import XCTest
@testable import IchorCore

final class DemoTourStepsTests: XCTestCase {
    func testFiveStepsInOrder() {
        XCTAssertEqual(DemoTourSteps.order, [.switcher, .nodes, .degradedNode, .actions, .settings])
        XCTAssertEqual(Set(DemoTourSteps.order), Set(DemoTourStep.allCases))
    }

    func testEveryStepHasItsKeys() {
        XCTAssertEqual(DemoTourStep.switcher.titleKey, "Switch clusters")
        XCTAssertEqual(DemoTourStep.nodes.titleKey, "Your nodes")
        XCTAssertEqual(DemoTourStep.degradedNode.titleKey, "A node needs a look")
        XCTAssertEqual(DemoTourStep.actions.titleKey, "Quick actions")
        XCTAssertEqual(DemoTourStep.settings.titleKey, "Settings")
        for step in DemoTourStep.allCases {
            XCTAssertFalse(step.titleKey.isEmpty)
            XCTAssertTrue(step.textKey.hasSuffix("."), step.rawValue)
        }
        XCTAssertEqual(Set(DemoTourStep.allCases.map(\.textKey)).count, 5)
    }

    func testRawValuesAreAndroidAnchors() {
        XCTAssertEqual(DemoTourStep.allCases.map(\.rawValue), ["SWITCHER", "NODES", "DEGRADED_NODE", "ACTIONS", "SETTINGS"])
    }

    func testOnlyTheTalosDemoRunsIt() {
        XCTAssertTrue(DemoTourSteps.runs(demo: true, kube: false))
        XCTAssertFalse(DemoTourSteps.runs(demo: true, kube: true))
        XCTAssertFalse(DemoTourSteps.runs(demo: false, kube: false))
    }

    func testMissingAnchorIsSkipped() {
        let all = Set(DemoTourStep.allCases)
        XCTAssertEqual(DemoTourSteps.shown(from: 0, available: all), 0)
        XCTAssertEqual(DemoTourSteps.next(after: 1, available: all), 2)
        let noDegraded = all.subtracting([.degradedNode])
        XCTAssertEqual(DemoTourSteps.next(after: 1, available: noDegraded), 3)
        XCTAssertEqual(DemoTourSteps.shown(from: 2, available: noDegraded), 3)
    }

    func testEndsAfterTheLastStep() {
        let all = Set(DemoTourStep.allCases)
        XCTAssertNil(DemoTourSteps.next(after: 4, available: all))
        XCTAssertNil(DemoTourSteps.shown(from: 0, available: []))
        XCTAssertEqual(DemoTourSteps.shown(from: -1, available: all), 0)
    }
}
