import XCTest
@testable import IchorCore

final class FreezeReminderPlanTests: XCTestCase {
    func testIdentifier() {
        let id = freezeReminderID(prefix: "argo-freeze|", cluster: "abc", namespace: "argocd", project: "default", window: "w1")
        XCTAssertEqual(id, "argo-freeze|abc|argocd/default/w1")
        XCTAssertTrue(id.hasPrefix("argo-freeze|abc|"))
        XCTAssertNotEqual(id, freezeReminderID(prefix: "argo-freeze|", cluster: "abc", namespace: "argocd", project: "default", window: "w2"))
    }

    func testDelay() {
        let now = Date(timeIntervalSince1970: 1_000)
        // Ends in 10 min: fires in 5.
        XCTAssertEqual(freezeReminderDelay(endsAt: 1_600_000, lead: 300, now: now), 300)
        // Inside the lead time, at its edge, or already over: nothing to schedule.
        XCTAssertNil(freezeReminderDelay(endsAt: 1_200_000, lead: 300, now: now))
        XCTAssertNil(freezeReminderDelay(endsAt: 1_301_000, lead: 300, now: now))
        XCTAssertNil(freezeReminderDelay(endsAt: 500_000, lead: 300, now: now))
        XCTAssertEqual(freezeReminderDelay(endsAt: 1_302_000, lead: 300, now: now), 2)
    }
}
