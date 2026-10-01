import XCTest
@testable import IchorCore

final class ServiceControlTests: XCTestCase {
    func testActionsDependOnState() {
        XCTAssertEqual(serviceActions(state: "Running"), [.restart, .stop])
        XCTAssertEqual(serviceActions(state: "running"), [.restart, .stop])
        XCTAssertEqual(serviceActions(state: "Finished"), [.restart, .start])
        XCTAssertEqual(serviceActions(state: "Failed"), [.restart, .start])
        XCTAssertEqual(ServiceAction.restart.rawValue, "restart")
    }

    func testCriticalServices() {
        for id in ["apid", "trustd", "etcd", "kubelet", "machined", "containerd", "cri"] {
            XCTAssertTrue(isCriticalService(id), id)
        }
        XCTAssertFalse(isCriticalService("udevd"))
        XCTAssertFalse(isCriticalService("ext-tailscale"))
    }

    func testServiceControlRoles() {
        XCTAssertTrue(ContextSummary(name: "a", roles: ["os:admin"]).allows(.serviceControl))
        XCTAssertTrue(ContextSummary(name: "o", roles: ["os:operator"]).allows(.serviceControl))
        XCTAssertFalse(ContextSummary(name: "r", roles: ["os:reader"]).allows(.serviceControl))
        XCTAssertFalse(ContextSummary(name: "b", roles: ["os:etcd:backup"]).allows(.serviceControl))
        XCTAssertEqual(Feature.serviceControl.minimumRole, "os:operator")
    }

    func testAppendCapped() {
        XCTAssertEqual(appendCapped([1, 2], [3], cap: 5), [1, 2, 3])
        XCTAssertEqual(appendCapped([1, 2, 3], [4, 5, 6], cap: 4), [3, 4, 5, 6])
        XCTAssertEqual(appendCapped([Int](), Array(0..<10), cap: 3), [7, 8, 9])
    }
}
