import XCTest
@testable import IchorCore

final class KubeEventsLiveTests: XCTestCase {
    private let pod = LiveKubeEventObject(kind: "Pod", group: "", version: "v1", resource: "pods", namespace: "shop", name: "web-1")

    private func row(_ key: String, lastSeen: Int64, count: Int = 1, reason: String = "BackOff", note: String = "") -> LiveKubeEvent {
        LiveKubeEvent(key: key, type: "Warning", reason: reason, note: note, regarding: pod, count: count, firstSeen: 1, lastSeen: lastSeen)
    }

    func testDecodesGoBatch() throws {
        let json = """
        {"reset":true,"upserts":[{"key":"0fe04095a1c19432","type":"Warning","reason":"BackOff",
        "note":"Back-off restarting failed container","regarding":{"kind":"Pod","apiVersion":"v1","group":"",
        "version":"v1","resource":"pods","namespaced":true,"namespace":"shop","name":"web-1"},"count":6,
        "firstSeen":1791626400000,"lastSeen":1791626940000,"source":"kubelet"}],"removed":[]}
        """
        let batch = try TalosJSON.decode(LiveKubeEventsBatch.self, from: json)
        XCTAssertTrue(batch.reset)
        XCTAssertEqual(batch.upserts.count, 1)
        let event = batch.upserts[0]
        XCTAssertTrue(event.isWarning)
        XCTAssertEqual(event.count, 6)
        XCTAssertEqual(event.lastSeen, 1_791_626_940_000)
        XCTAssertEqual(event.regarding.label, "Pod shop/web-1")
        XCTAssertEqual(event.source, "kubelet")
    }

    func testDecodesStatusWithDefaults() throws {
        let status = try TalosJSON.decode(LiveKubeEventsStatus.self, from: #"{"state":"reconnecting","reason":"EOF"}"#)
        XCTAssertEqual(status.liveState, .reconnecting)
        XCTAssertEqual(status.reason, "EOF")
        XCTAssertEqual(status.dropped, 0)
        XCTAssertEqual(try TalosJSON.decode(LiveKubeEventsStatus.self, from: #"{"state":"newer"}"#).liveState, .live)
    }

    func testResetReplacesEverything() {
        let rows = [row("a", lastSeen: 5)]
        let applied = applyKubeEventsBatch(LiveKubeEventsBatch(reset: true, upserts: [row("b", lastSeen: 1)]), to: rows)
        XCTAssertEqual(applied.map(\.key), ["b"])
    }

    func testRemovedThenUpsertedAndSortedNewestFirst() {
        let rows = [row("a", lastSeen: 30), row("b", lastSeen: 20), row("c", lastSeen: 10)]
        let batch = LiveKubeEventsBatch(upserts: [row("c", lastSeen: 40, count: 3), row("d", lastSeen: 25)], removed: ["b", "zz"])
        let applied = applyKubeEventsBatch(batch, to: rows)
        XCTAssertEqual(applied.map(\.key), ["c", "a", "d"])
        XCTAssertEqual(applied.first?.count, 3)
    }

    func testRemovedKeyUpsertedInSameBatchStays() {
        let applied = applyKubeEventsBatch(LiveKubeEventsBatch(upserts: [row("a", lastSeen: 9)], removed: ["a"]), to: [row("a", lastSeen: 1)])
        XCTAssertEqual(applied.map(\.lastSeen), [9])
    }

    func testTieBreaksByKeyAndCaps() {
        let applied = applyKubeEventsBatch(LiveKubeEventsBatch(upserts: [row("b", lastSeen: 1), row("a", lastSeen: 1), row("c", lastSeen: 2)]),
                                           to: [], cap: 2)
        XCTAssertEqual(applied.map(\.key), ["c", "a"])
    }

    func testPauseHoldsBatchesAndResumeAppliesThem() {
        let first = KubeEventsFeed().receiving(LiveKubeEventsBatch(reset: true, upserts: [row("a", lastSeen: 1), row("b", lastSeen: 2)]))
        XCTAssertTrue(first.loaded)
        let paused = first.pausing()
            .receiving(LiveKubeEventsBatch(upserts: [row("a", lastSeen: 5, count: 2), row("c", lastSeen: 3)]))
            .receiving(LiveKubeEventsBatch(upserts: [row("a", lastSeen: 6, count: 3)], removed: ["b", "c"]))
        XCTAssertEqual(paused.rows.map(\.key), ["b", "a"])
        XCTAssertEqual(paused.pendingCount, 1)
        let resumed = paused.resuming()
        XCTAssertFalse(resumed.paused)
        XCTAssertNil(resumed.pending)
        XCTAssertEqual(resumed.rows.map(\.key), ["a"])
        XCTAssertEqual(resumed.rows.first?.count, 3)
    }

    func testFirstBatchShowsEvenWhenPaused() {
        let feed = KubeEventsFeed().pausing().receiving(LiveKubeEventsBatch(reset: true, upserts: [row("a", lastSeen: 1)]))
        XCTAssertEqual(feed.rows.map(\.key), ["a"])
        XCTAssertEqual(feed.pendingCount, 0)
    }

    func testResetWhilePausedReplacesWhatWasHeld() {
        let feed = KubeEventsFeed().receiving(LiveKubeEventsBatch(reset: true, upserts: [row("a", lastSeen: 1)])).pausing()
            .receiving(LiveKubeEventsBatch(upserts: [row("b", lastSeen: 2)]))
            .receiving(LiveKubeEventsBatch(reset: true, upserts: [row("c", lastSeen: 3)]))
        XCTAssertEqual(feed.resuming().rows.map(\.key), ["c"])
    }

    func testMergedMatchesApplyingInTurn() {
        let rows = [row("a", lastSeen: 1), row("b", lastSeen: 2)]
        let one = LiveKubeEventsBatch(upserts: [row("c", lastSeen: 3)], removed: ["a"])
        let two = LiveKubeEventsBatch(upserts: [row("a", lastSeen: 4)], removed: ["c"])
        XCTAssertEqual(applyKubeEventsBatch(one.merged(with: two), to: rows),
                       applyKubeEventsBatch(two, to: applyKubeEventsBatch(one, to: rows)))
    }

    func testRegardingOpensItsResource() {
        let resource = pod.apiResource
        XCTAssertEqual(resource?.resource, "pods")
        XCTAssertEqual(resource?.kind, "Pod")
        XCTAssertEqual(pod.objectNamespace, "shop")
        let node = LiveKubeEventObject(kind: "Node", resource: "nodes", namespaced: false, namespace: "ignored", name: "worker-1")
        XCTAssertEqual(node.objectNamespace, "")
        XCTAssertEqual(node.label, "Node worker-1")
        XCTAssertEqual(node.apiResource?.namespaced, false)
        let deployment = LiveKubeEventObject(kind: "Deployment", apiVersion: "apps/v1", group: "apps", version: "v1",
                                             resource: "deployments", namespace: "shop", name: "web")
        XCTAssertEqual(deployment.apiResource?.group, "apps")
    }

    func testUnknownKindDoesNotNavigate() {
        XCTAssertNil(LiveKubeEventObject(kind: "Widget", apiVersion: "example.com/v1", resource: "", name: "w").apiResource)
        XCTAssertNil(LiveKubeEventObject(kind: "Pod", resource: "pods", name: "").apiResource)
    }

    func testSearchMatchesReasonObjectAndNote() {
        let event = row("a", lastSeen: 1, reason: "Unhealthy", note: "Readiness probe failed")
        XCTAssertTrue(event.matches(""))
        XCTAssertTrue(event.matches("unhealthy"))
        XCTAssertTrue(event.matches("shop/web"))
        XCTAssertTrue(event.matches("PROBE"))
        XCTAssertFalse(event.matches("evicted"))
    }

    func testShareText() {
        let rows = [row("a", lastSeen: 1_791_626_940_000, count: 6, note: "Back-off restarting"), row("b", lastSeen: 0, reason: "Pulled")]
        let text = kubeEventsShareText(rows, header: "Events · shop")
        XCTAssertEqual(text, """
        Events · shop

        2026-10-10T10:09:00Z Warning BackOff Pod shop/web-1 ×6
            Back-off restarting
        1970-01-01T00:00:00Z Warning Pulled Pod shop/web-1
        """)
        XCTAssertEqual(kubeEventsShareText([]), "")
    }
}
