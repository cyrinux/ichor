import XCTest
@testable import IchorCore

final class ApiHealthTests: XCTestCase {
    // A KubeAPIHealth answer (go/ichorgo/kube_apihealth.go), cut down from the demo's.
    private let json = #"""
    {"status":"busy","version":"v1.34.1",
     "ready":{"ok":true,"checks":[{"name":"ping","ok":true},{"name":"etcd","ok":true}]},
     "live":{"ok":false,"checks":[{"name":"ping","ok":true},{"name":"etcd","reason":"reason withheld"}]},
     "uptimeSeconds":950400,"windowSeconds":5,"requestRate":142.6,"errorRate":0.2,"throttledRate":0,"rejectedRate":0,
     "inflightRead":18,"inflightMutate":3,"queued":4,"watches":612,"watchEventRate":88.4,"etcdLatencyMs":6.8,
     "clients":[{"name":"service-accounts","priority":"workload-low","rate":61.2,"rejectedRate":0,"queued":4,"waitMs":140}],
     "priorities":[{"name":"workload-low","executing":21,"limit":24,"queued":4,"rejectedRate":0},{"name":"exempt","executing":2,"limit":0}],
     "requests":[{"verb":"LIST","resource":"pods","rate":48.6,"errorRate":0,"latencyMs":412},{"verb":"GET","resource":"","rate":4.2}],
     "watchedKinds":[{"resource":"pods","count":148}],
     "objects":[{"resource":"events","count":4210}],
     "queuedRequests":[{"user":"system:serviceaccount:monitoring:pod-exporter","flowSchema":"service-accounts","priority":"workload-low","verb":"list","path":"/api/v1/pods"}],
     "futureField":true}
    """#

    private func decode(_ text: String) throws -> ApiHealthReport {
        try JSONDecoder().decode(ApiHealthReport.self, from: Data(text.utf8))
    }

    func testDecodesTheReport() throws {
        let report = try decode(json)
        XCTAssertEqual(report.status, .busy)
        XCTAssertTrue(report.ratesAreLive)
        XCTAssertEqual(report.clients.map(\.name), ["service-accounts"])
        XCTAssertEqual(report.requests[1].resource, "")
        XCTAssertEqual(report.requests[1].latencyMs, 0)
        XCTAssertEqual(report.queuedRequests.first?.user, "system:serviceaccount:monitoring:pod-exporter")
    }

    func testFailedChecksAreListedOnce() throws {
        let failed = try decode(json).failedChecks
        XCTAssertEqual(failed.map(\.name), ["etcd"])
        XCTAssertEqual(failed.first?.reason, "reason withheld")
    }

    func testPriorityShareIsNilWhenExempt() throws {
        let levels = try decode(json).priorities
        XCTAssertEqual(levels[0].share ?? 0, 0.875, accuracy: 0.0001)
        XCTAssertNil(levels[1].share)
    }

    func testUnknownStatusAndMissingFieldsDefault() throws {
        let bare = try decode(#"{"status":"new","metricsError":"forbidden"}"#)
        XCTAssertEqual(bare.status, .ok)
        XCTAssertFalse(bare.ratesAreLive)
        XCTAssertTrue(bare.clients.isEmpty)
        XCTAssertEqual(bare.metricsError, "forbidden")
    }

    func testFormatsRatesAndDurations() {
        XCTAssertEqual(formatRate(142.4), "142/s")
        XCTAssertEqual(formatRate(3.6), "3.6/s")
        XCTAssertEqual(formatRate(0.0201), "0.02/s")
        XCTAssertEqual(formatRate(0), "0/s")
        XCTAssertEqual(formatMs(412.2), "412 ms")
        XCTAssertEqual(formatMs(6.8), "6.8 ms")
    }
}
