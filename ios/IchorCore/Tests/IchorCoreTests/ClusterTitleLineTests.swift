import XCTest
@testable import IchorCore

final class ClusterTitleLineTests: XCTestCase {
    func testOneClusterNeedsNoLine() {
        XCTAssertNil(clusterTitleLine(clusterCount: 1, activeLabel: "Lab"))
        XCTAssertEqual(clusterTitleLine(clusterCount: 1, activeLabel: "Lab", subtitle: "node-a"), "node-a")
    }

    func testSeveralClustersNameTheActiveOne() {
        XCTAssertEqual(clusterTitleLine(clusterCount: 2, activeLabel: "Lab"), "Lab")
        XCTAssertEqual(clusterTitleLine(clusterCount: 3, activeLabel: "Lab", subtitle: "node-a"), "node-a · Lab")
        XCTAssertEqual(clusterTitleLine(clusterCount: 2, activeLabel: "Lab", subtitle: " "), "Lab")
    }

    func testNoActiveClusterOnlyTheSubtitle() {
        XCTAssertNil(clusterTitleLine(clusterCount: 2, activeLabel: nil))
        XCTAssertNil(clusterTitleLine(clusterCount: 2, activeLabel: ""))
    }
}
