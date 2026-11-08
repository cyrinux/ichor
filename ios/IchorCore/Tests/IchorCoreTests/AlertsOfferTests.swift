import XCTest
@testable import IchorCore

final class AlertsOfferTests: XCTestCase {
    private var defaults: UserDefaults!

    override func setUp() {
        defaults = UserDefaults(suiteName: "AlertsOfferTests-\(UUID().uuidString)")
    }

    func testShownOnlyForAPendingOfferOnARealClusterWithAlertsOff() {
        XCTAssertTrue(AlertsOffer.shouldShow(pending: true, alertsEnabled: false, isDemo: false, tourActive: false))
        XCTAssertFalse(AlertsOffer.shouldShow(pending: false, alertsEnabled: false, isDemo: false, tourActive: false))
        XCTAssertFalse(AlertsOffer.shouldShow(pending: true, alertsEnabled: true, isDemo: false, tourActive: false))
        XCTAssertFalse(AlertsOffer.shouldShow(pending: true, alertsEnabled: false, isDemo: true, tourActive: false))
        XCTAssertFalse(AlertsOffer.shouldShow(pending: true, alertsEnabled: false, isDemo: false, tourActive: true))
    }

    func testOfferedOnceAndNeverAgainAfterDismiss() {
        AlertsOffer.offerOnce(defaults)
        XCTAssertTrue(defaults.bool(forKey: AlertsOffer.pendingKey))
        AlertsOffer.dismiss(defaults)
        XCTAssertFalse(defaults.bool(forKey: AlertsOffer.pendingKey))
        AlertsOffer.offerOnce(defaults)
        XCTAssertFalse(defaults.bool(forKey: AlertsOffer.pendingKey), "a second cluster does not bring it back")
    }
}
