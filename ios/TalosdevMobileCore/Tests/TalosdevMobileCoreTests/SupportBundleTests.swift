import XCTest
@testable import TalosdevMobileCore

final class SupportBundleTests: XCTestCase {
    func testProgressDecoding() throws {
        let progress = try TalosJSON.decode(SupportProgress.self, from: #"{"node":"10.0.0.1","step":"dmesg","done":3,"total":12}"#)
        XCTAssertEqual(progress, SupportProgress(node: "10.0.0.1", step: "dmesg", done: 3, total: 12))
        XCTAssertEqual(progress.fraction, 0.25)
        XCTAssertFalse(progress.finished)
        XCTAssertNil(try TalosJSON.decode(SupportProgress.self, from: #"{"node":"n"}"#).fraction)
        XCTAssertTrue(SupportProgress(node: "n", done: 12, total: 12).finished)
        XCTAssertEqual(SupportProgress(node: "n", done: 15, total: 12).fraction, 1)
        XCTAssertFalse(SupportProgress(node: "n").finished)
    }

    func testProgressPerNode() {
        var progress = SupportBundleProgress()
        XCTAssertEqual(progress.state(of: "a"), .waiting)

        // done and total count the node's own steps; a step is reported when it starts.
        progress = progress.recording(SupportProgress(node: "a", step: "version", done: 0, total: 8))
        progress = progress.recording(SupportProgress(node: "a", step: "kernel log", done: 1, total: 8))
        XCTAssertEqual(progress.state(of: "a"), .collecting(SupportProgress(node: "a", step: "kernel log", done: 1, total: 8)))
        XCTAssertEqual(progress.state(of: "b"), .waiting)
        XCTAssertEqual(progress.state(of: ""), .waiting)

        progress = progress.recording(SupportProgress(node: "b", step: "version", done: 0, total: 8))
        progress = progress.recording(SupportProgress(node: "a", step: "processes", done: 7, total: 8))
        XCTAssertEqual(progress.state(of: "a"), .collecting(SupportProgress(node: "a", step: "processes", done: 7, total: 8)))
        // The node's closing report (step "done", done == total) finishes it; it is not a step to show.
        progress = progress.recording(SupportProgress(node: "a", step: "done", done: 8, total: 8))
        XCTAssertEqual(progress.state(of: "a"), .done)
        XCTAssertEqual(progress.state(of: "b"), .collecting(SupportProgress(node: "b", step: "version", done: 0, total: 8)))

        // The cluster-wide part reports under no node, as one step.
        progress = progress.recording(SupportProgress(node: "", step: "etcd", done: 0, total: 1))
        XCTAssertEqual(progress.state(of: ""), .collecting(SupportProgress(node: "", step: "etcd", done: 0, total: 1)))
        progress = progress.recording(SupportProgress(node: "", step: "done", done: 1, total: 1))
        XCTAssertEqual(progress.state(of: ""), .done)
        XCTAssertFalse(progress.closed)
        // a and the cluster part are done, b is not: two rows of three.
        XCTAssertEqual(progress.fractionDone(nodes: ["a", "b"]), 2.0 / 3.0, accuracy: 0.0001)
        XCTAssertEqual(SupportBundleProgress().fractionDone(nodes: ["a"]), 0)
        XCTAssertEqual(progress.state(of: "b"), .collecting(SupportProgress(node: "b", step: "version", done: 0, total: 8)))
        XCTAssertFalse(progress.countsWholeBundle)
    }

    func testProgressCountedOverTheWholeBundle() {
        // An older core: one count for all sections, collected one after the other.
        var progress = SupportBundleProgress()
        progress = progress.recording(SupportProgress(node: "a", step: "version", done: 0, total: 5))
        progress = progress.recording(SupportProgress(node: "a", step: "kernel log", done: 1, total: 5))
        XCTAssertFalse(progress.countsWholeBundle)
        XCTAssertEqual(progress.state(of: "a"), .collecting(SupportProgress(node: "a", step: "kernel log", done: 1, total: 5)))

        // b starts with steps already done: the count is the bundle's, so a is done.
        progress = progress.recording(SupportProgress(node: "b", step: "version", done: 2, total: 5))
        XCTAssertTrue(progress.countsWholeBundle)
        XCTAssertEqual(progress.state(of: "a"), .done)
        XCTAssertEqual(progress.state(of: "b"), .collecting(SupportProgress(node: "b", step: "version", done: 2, total: 5)))
        XCTAssertEqual(progress.state(of: ""), .waiting)

        progress = progress.recording(SupportProgress(node: "", step: "etcd", done: 4, total: 5))
        XCTAssertEqual(progress.state(of: "b"), .done)
        XCTAssertEqual(progress.state(of: ""), .collecting(SupportProgress(node: "", step: "etcd", done: 4, total: 5)))

        // The closing report finishes everything.
        progress = progress.recording(SupportProgress(node: "", step: "done", done: 5, total: 5))
        XCTAssertTrue(progress.closed)
        XCTAssertEqual(progress.state(of: ""), .done)
        XCTAssertEqual(progress.state(of: "a"), .done)
    }

    func testStalePartFiles() {
        XCTAssertTrue(isStaleSupportPartName("support-homelab-20261001-143005.zip.part"))
        XCTAssertFalse(isStaleSupportPartName("support-homelab-20261001-143005.zip"))
        XCTAssertFalse(isStaleSupportPartName("other.part"))
        XCTAssertFalse(isStaleSupportPartName(".part"))
    }

    func testNodesCSV() {
        XCTAssertEqual(supportNodesCSV(all: ["a", "b", "c"], selected: ["c", "a"]), "a,c")
        XCTAssertEqual(supportNodesCSV(all: ["a", "b"], selected: []), "")
        XCTAssertEqual(supportNodesCSV(all: ["a"], selected: ["zzz"]), "")
    }

    func testFilename() {
        let utc = TimeZone(identifier: "UTC")!
        let date = Date(timeIntervalSince1970: 1_790_865_005) // 2026-10-01 14:30:05 UTC
        XCTAssertEqual(supportBundleFilename(context: "homelab", date: date, timeZone: utc), "support-homelab-20261001-143005.zip")
        XCTAssertEqual(supportBundleFilename(context: "admin@prod/eu west", date: date, timeZone: utc), "support-admin-prod-eu-west-20261001-143005.zip")
        XCTAssertEqual(supportBundleFilename(context: "…", date: date, timeZone: utc), "support-cluster-20261001-143005.zip")
        XCTAssertEqual(supportBundleFilename(context: String(repeating: "x", count: 60), date: date, timeZone: utc),
                       "support-\(String(repeating: "x", count: 40))-20261001-143005.zip")
    }

    func testBundleNames() {
        XCTAssertTrue(isSupportBundleName("support-homelab-20261001-143005.zip"))
        XCTAssertFalse(isSupportBundleName("support-.zip"))
        XCTAssertFalse(isSupportBundleName("capture.pcap"))
        XCTAssertFalse(isSupportBundleName("support-a b.zip"))
        XCTAssertFalse(isSupportBundleName("support-../x.zip"))
    }

    func testSorting() {
        let old = SupportBundleFile(name: "support-a.zip", size: 1, modified: Date(timeIntervalSince1970: 10))
        let new = SupportBundleFile(name: "support-b.zip", size: 1, modified: Date(timeIntervalSince1970: 20))
        XCTAssertEqual(sortSupportBundles([old, new]).map(\.name), ["support-b.zip", "support-a.zip"])
    }
}
