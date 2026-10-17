import XCTest
@testable import IchorCore

final class MaskTransitionTests: XCTestCase {
    private struct Failure: Error {}

    func testStepsRunInOrder() {
        XCTAssertEqual(run(failing: false), ["begin", "drop", "mask", "reread", "end"])
    }

    func testEndRunsWhenRereadThrows() {
        XCTAssertEqual(run(failing: true), ["begin", "drop", "mask", "reread", "end"])
    }

    func testPathKeepsOnlyTheSettingsOnTop() {
        let isSettings: (String) -> Bool = { $0.hasPrefix("settings") }
        XCTAssertEqual(pathKeptOnMaskSwitch(["node", "settings", "settings.privacy"], isSettings: isSettings),
                       ["settings", "settings.privacy"])
        XCTAssertEqual(pathKeptOnMaskSwitch(["settings", "node", "settings"], isSettings: isSettings), ["settings"])
        XCTAssertEqual(pathKeptOnMaskSwitch(["node", "logs"], isSettings: isSettings), [])
        XCTAssertEqual(pathKeptOnMaskSwitch([], isSettings: isSettings), [])
    }

    func testAlertNotificationsAreTheOnesRemoved() {
        XCTAssertTrue(notificationNamesClusters(category: alertCategory(kind: "node", actions: [], hideDetails: false), identifier: "a"))
        XCTAssertTrue(notificationNamesClusters(category: alertCategory(kind: "etcd", actions: [], hideDetails: true), identifier: "a"))
        XCTAssertTrue(notificationNamesClusters(category: "private", identifier: "a"))
        XCTAssertTrue(notificationNamesClusters(category: "", identifier: "wake|demo|node"))
        XCTAssertFalse(notificationNamesClusters(category: "freeze", identifier: "freeze-1"))
        XCTAssertFalse(notificationNamesClusters(category: "", identifier: "upgrade.foreground"))
    }

    /// The Linux test harness runs synchronous tests only: wait for the main actor's work.
    private func run(failing: Bool) -> [String] {
        let done = expectation(description: "transition")
        var order: [String] = []
        Task { @MainActor in
            order = await MaskTransitionTests.record(failing: failing)
            done.fulfill()
        }
        wait(for: [done], timeout: 5)
        return order
    }

    @MainActor
    private static func record(failing: Bool) async -> [String] {
        var order: [String] = []
        do {
            try await MaskTransition.run(
                begin: { order.append("begin") },
                dropData: { order.append("drop") },
                applyMask: { order.append("mask") },
                reread: {
                    order.append("reread")
                    if failing { throw Failure() }
                },
                end: { order.append("end") }
            )
            XCTAssertFalse(failing)
        } catch {
            XCTAssertTrue(failing)
        }
        return order
    }
}
