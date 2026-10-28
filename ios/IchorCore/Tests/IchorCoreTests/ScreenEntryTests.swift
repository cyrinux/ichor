import XCTest
@testable import IchorCore

final class ScreenEntryTests: XCTestCase {
    func testDecodesTheGoResult() throws {
        let json = #"{"results":[{"id":"etcd","title":"etcd","subtitle":"","section":"Cluster","score":1}],"total":3}"#
        let matches = try TalosJSON.decode(ScreenMatches.self, from: json)
        XCTAssertEqual(matches.total, 3)
        XCTAssertEqual(matches.results.map(\.id), ["etcd"])
        XCTAssertEqual(matches.results.first?.section, "Cluster")
        XCTAssertEqual(matches.results.first?.score, 1)
    }

    func testMissingFieldsDefault() throws {
        let matches = try TalosJSON.decode(ScreenMatches.self, from: #"{"results":[{"id":"runbooks","title":"Runbooks"}]}"#)
        XCTAssertEqual(matches.total, 1)
        XCTAssertEqual(matches.results.first?.subtitle, "")
        XCTAssertEqual(matches.results.first?.score, 0)
    }

    func testEncodesTheCatalogAsGoTakesIt() throws {
        let entries = ScreenEntries(screens: [ScreenEntry(id: "settings/alerts", title: "Settings → Alerts", section: "Settings")])
        let decoded = try TalosJSON.decode(ScreenEntries.self, from: entries.json)
        XCTAssertEqual(decoded, entries)
        XCTAssertTrue(entries.json.contains(#""screens""#))
    }

    func testNodesComeFromTheActiveClustersOverview() throws {
        let overview = ClusterOverview(context: "lab", nodes: [NodeOverview(node: "10.0.0.1", hostname: "node-a", reachable: true)])
        let blob = try TalosJSON.encode(overview)
        let entries = [
            SearchEntry(cluster: "a", context: "other", key: "overview", at: Int64(0), json: blob),
            SearchEntry(cluster: "b", context: "lab", key: "pods|*", at: Int64(0), json: "{}"),
        ]
        XCTAssertTrue(searchOverviewNodes(entries, context: "lab").isEmpty)
        let active = entries + [SearchEntry(cluster: "b", context: "lab", key: "overview", at: Int64(0), json: blob)]
        XCTAssertEqual(searchOverviewNodes(active, context: "lab").map(\.hostname), ["node-a"])
    }
}
