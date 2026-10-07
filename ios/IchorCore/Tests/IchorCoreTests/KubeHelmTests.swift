import XCTest
@testable import IchorCore

final class KubeHelmTests: XCTestCase {
    func testDecodesList() throws {
        let list = try TalosJSON.decode(HelmReleaseList.self, from: """
        {"releases":[{"name":"cert-manager","namespace":"cert-manager","revision":4,"status":"deployed",
          "chart":"cert-manager","chartVersion":"v1.18.2","appVersion":"v1.18.2","updated":1790000000}]}
        """)
        XCTAssertEqual(list.releases.count, 1)
        XCTAssertEqual(list.releases[0].chartLabel, "cert-manager-v1.18.2")
        XCTAssertEqual(list.releases[0].tone, .good)
        XCTAssertEqual(list.releases[0].id, "cert-manager/cert-manager")
        XCTAssertEqual(try TalosJSON.decode(HelmReleaseList.self, from: #"{"releases":null}"#), HelmReleaseList())
    }

    func testDecodesDetailWithFlatSummaryAndNewestFirstHistory() throws {
        let detail = try TalosJSON.decode(HelmReleaseDetail.self, from: """
        {"name":"web","namespace":"apps","revision":3,"status":"failed","chart":"web","chartVersion":"1.0.0",
         "updated":1790000000,"description":"Upgrade failed","notes":"Visit http://web","values":"replicaCount: 2\\n",
         "manifest":"---\\nkind: Service\\n",
         "history":[{"revision":1,"status":"superseded","updated":1},{"revision":3,"status":"failed","updated":3,"description":"boom"},
                    {"revision":2,"status":"superseded","updated":2}]}
        """)
        XCTAssertEqual(detail.summary.name, "web")
        XCTAssertEqual(detail.summary.tone, .bad)
        XCTAssertEqual(detail.summary.appVersion, "")
        XCTAssertEqual(detail.description, "Upgrade failed")
        XCTAssertEqual(detail.values, "replicaCount: 2\n")
        XCTAssertEqual(detail.history.map(\.revision), [3, 2, 1])
        XCTAssertEqual(detail.history[0].description, "boom")
    }

    func testFilterAndSort() {
        let releases = [
            HelmReleaseSummary(name: "web", namespace: "apps", chart: "nginx"),
            HelmReleaseSummary(name: "db", namespace: "data", status: "failed", chart: "postgresql"),
            HelmReleaseSummary(name: "api", namespace: "apps", status: "pending-upgrade", chart: "api"),
            HelmReleaseSummary(name: "cache", namespace: "apps", chart: "redis"),
        ]
        XCTAssertEqual(sortHelmReleases(releases).map(\.name), ["db", "api", "cache", "web"])
        XCTAssertEqual(filterHelmReleases(releases, query: "POSTGRES").map(\.name), ["db"])
        XCTAssertEqual(filterHelmReleases(releases, query: "apps").map(\.name), ["web", "api", "cache"])
        XCTAssertEqual(filterHelmReleases(releases, query: " ").count, 4)
    }
}
