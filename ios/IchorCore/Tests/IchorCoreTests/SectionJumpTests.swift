import XCTest
@testable import IchorCore

final class SectionJumpTests: XCTestCase {
    private let sections = [
        JumpSection(id: "summary", label: "Summary"),
        JumpSection(id: "nodes", label: "Nodes"),
        JumpSection(id: "workloads", label: "Workloads"),
    ]

    func testOneHeaderAtTheTopIsCurrent() {
        XCTAssertEqual(
            SectionJump.current(sections: sections, visibleTops: ["nodes": 100], top: 100, previous: nil), "nodes")
    }

    func testTheLastReachedHeaderWinsOverEarlierOnes() {
        let tops = ["summary": -300.0, "nodes": 90, "workloads": 600]
        XCTAssertEqual(
            SectionJump.current(sections: sections, visibleTops: tops, top: 100, previous: "summary"), "nodes")
    }

    func testHeadersAllBelowTheTopMeanTheSectionBeforeThem() {
        // Scrolling back up through the nodes: only the workloads header is in view, low on screen.
        XCTAssertEqual(
            SectionJump.current(sections: sections, visibleTops: ["workloads": 500], top: 100, previous: "workloads"),
            "nodes")
        XCTAssertEqual(
            SectionJump.current(sections: sections, visibleTops: ["summary": 300], top: 100, previous: nil), "summary")
    }

    func testNoHeaderInViewKeepsThePrevious() {
        XCTAssertEqual(SectionJump.current(sections: sections, visibleTops: [:], top: 100, previous: "nodes"), "nodes")
    }

    func testNoSectionsIsNil() {
        XCTAssertNil(SectionJump.current(sections: [], visibleTops: ["nodes": 0], top: 0, previous: "nodes"))
    }

    func testUnknownIdsAreIgnored() {
        XCTAssertEqual(
            SectionJump.current(sections: sections, visibleTops: ["other": 0], top: 0, previous: "summary"), "summary")
    }

    func testSectionAtMapsTheEndsAndClamps() {
        XCTAssertEqual(SectionJump.section(at: 0, in: sections)?.id, "summary")
        XCTAssertEqual(SectionJump.section(at: 1, in: sections)?.id, "workloads")
        XCTAssertEqual(SectionJump.section(at: -0.5, in: sections)?.id, "summary")
        XCTAssertEqual(SectionJump.section(at: 4, in: sections)?.id, "workloads")
        XCTAssertEqual(SectionJump.section(at: 0.5, in: sections)?.id, "nodes")
        XCTAssertEqual(SectionJump.section(at: .nan, in: sections)?.id, "summary")
    }

    func testTwoSectionsSplitInHalf() {
        let two = Array(sections.prefix(2))
        XCTAssertEqual(SectionJump.section(at: 0.49, in: two)?.id, "summary")
        XCTAssertEqual(SectionJump.section(at: 0.5, in: two)?.id, "nodes")
    }

    func testSectionAtOfNoSectionsIsNil() {
        XCTAssertNil(SectionJump.section(at: 0.5, in: []))
    }
}
