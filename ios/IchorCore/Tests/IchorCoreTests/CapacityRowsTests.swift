import XCTest
@testable import IchorCore

final class CapacityRowsTests: XCTestCase {
    private func workloads(_ json: String) throws -> [CapacityWorkload] {
        try JSONDecoder().decode([CapacityWorkload].self, from: Data(json.utf8))
    }

    func testMajorityNeedsMoreThanHalf() {
        XCTAssertEqual(majorityValue([1, 1, 2]), 1)
        XCTAssertNil(majorityValue([1, 1, 2, 2]))
        XCTAssertNil(majorityValue([Int]()))
        XCTAssertEqual(majorityValue(["a"]), "a")
    }

    func testDetailsHideWhatMostWorkloadsShare() throws {
        let w = try workloads(#"""
        [{"kind":"Deployment","namespace":"a","name":"one","priorityClassName":"platform-optional","priority":10,"qos":"Burstable"},
         {"kind":"Deployment","namespace":"a","name":"two","priorityClassName":"platform-optional","priority":10,"qos":"Burstable"},
         {"kind":"Deployment","namespace":"a","name":"three","noPriorityClass":true,"priority":0,"qos":"BestEffort"},
         {"kind":"StatefulSet","namespace":"a","name":"four","priorityClassName":"platform-optional","priority":10,"qos":"Guaranteed"},
         {"kind":"DaemonSet","namespace":"a","name":"five","priorityClassName":"platform-optional","priority":10,"qos":"Burstable"}]
        """#)
        let majority = CapacityMajority(w)
        XCTAssertEqual(majority.priorityClass, "platform-optional")
        XCTAssertEqual(majority.priority, 10)
        XCTAssertEqual(majority.qos, .burstable)
        XCTAssertEqual(w[0].details(against: majority), [])
        XCTAssertEqual(w[2].details(against: majority), [.noPriorityClass, .priority(0), .qos(.bestEffort)])
        XCTAssertEqual(w[3].details(against: majority), [.qos(.guaranteed)])
    }

    func testWithoutAMajorityEverythingShows() throws {
        let w = try workloads(#"""
        [{"kind":"Deployment","namespace":"a","name":"one","priorityClassName":"high","priority":100,"qos":"Burstable"},
         {"kind":"Deployment","namespace":"a","name":"two","priorityClassName":"low","priority":1,"qos":"Guaranteed"},
         {"kind":"Deployment","namespace":"a","name":"three","qos":""}]
        """#)
        let majority = CapacityMajority(w)
        XCTAssertNil(majority.priorityClass)
        XCTAssertEqual(w[0].details(against: majority), [.priorityClass("high"), .priority(100), .qos(.burstable)])
        // No class name to show, and an unknown QoS is never shown.
        XCTAssertEqual(w[2].details(against: majority), [.priority(0)])
    }

    func testNodeFlagsRolesAndOrder() throws {
        let nodes = try JSONDecoder().decode([CapacityNode].self, from: Data(#"""
        [{"name":"n1","roles":["storage","control-plane"],"diskPressure":true,"diskMeasured":true,"diskCapacity":100,"diskAvailable":5},
         {"name":"n2","overcommitted":true,"diskPressure":true},
         {"name":"n3","diskMeasured":true,"diskCapacity":100,"diskAvailable":5},
         {"name":"n4"},
         {"name":"n5","roles":["garage","longhorn"]}]
        """#.utf8))
        XCTAssertEqual(nodes.map(\.flag), [.diskPressure, .overcommitted, .diskLow, nil, nil])
        XCTAssertEqual(nodes[0].roleLine, "control-plane · storage")
        XCTAssertEqual(nodes[3].roleLine, "")
        XCTAssertEqual(nodes.overcommittedFirst.map(\.name), ["n2", "n1", "n3", "n4", "n5"])
        XCTAssertEqual(nodes[4].roleLine, "garage, longhorn")
    }
}
