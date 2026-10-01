import XCTest
@testable import TalosdevMobileCore

final class AppLockStateTests: XCTestCase {
    func testColdStartLockedOnlyWhenEnabled() {
        XCTAssertTrue(AppLockState(enabled: true).locked)
        XCTAssertFalse(AppLockState(enabled: false).locked)
    }

    func testRelocksAfterGrace() {
        var lock = AppLockState(enabled: true, grace: 30)
        lock.unlock()
        lock.onBackground(now: 0)
        lock.onForeground(now: 29.9)
        XCTAssertFalse(lock.locked, "short trips must not relock")
        lock.onBackground(now: 100)
        lock.onForeground(now: 130)
        XCTAssertTrue(lock.locked)
    }

    func testForegroundWithoutBackgroundDoesNothing() {
        var lock = AppLockState(enabled: true)
        lock.unlock()
        lock.onForeground(now: 9_999)
        XCTAssertFalse(lock.locked)
    }

    func testDisablingUnlocksAndNeverRelocks() {
        var lock = AppLockState(enabled: true)
        lock.setEnabled(false)
        XCTAssertFalse(lock.locked)
        lock.onBackground(now: 0)
        lock.onForeground(now: 9_999)
        XCTAssertFalse(lock.locked)
    }
}
