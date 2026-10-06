import XCTest
@testable import IchorCore

final class CheckupTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)
    private var farCert: Int64 { Int64(now.timeIntervalSince1970) + 365 * 86_400 }

    // A KubeCheckup answer (go/ichorgo/kube_checkup.go), cut down.
    private let json = #"""
    {"status":"critical","kubeVersion":"v1.34.1","sections":[
      {"id":"workloads","status":"critical","checked":8,"findings":[
        {"kind":"podCrashLoop","severity":"critical","namespace":"shop","name":"web-1","count":9,"reason":"Error (exit 2)","message":"back-off"},
        {"kind":"podOOMKilled","severity":"warning","namespace":"shop","name":"cache-0","count":2},
        {"kind":"podFailed","severity":"info","namespace":"shop","name":"old","reason":"Evicted"},
        {"kind":"fromTheFuture","severity":"warning","name":"x"}]},
      {"id":"events","status":"ok","checked":3,"findings":[{"kind":"event","severity":"info","namespace":"shop","name":"web-1","extra":"Pod"}]},
      {"id":"nodes","status":"warning","checked":2,"findings":[{"kind":"nodeCordoned","severity":"warning","name":"n2"}]},
      {"id":"storage","status":"unknown","error":"forbidden","checked":0,"findings":[]},
      {"id":"helm","status":"absent","checked":0,"findings":[]}],
     "nodes":[{"name":"n1","roles":["worker"],"ready":true,"cpuRequests":3.8,"cpuAllocatable":4,"cpuPercent":95,"pods":2,"podCapacity":110}],
     "volumes":[{"namespace":"shop","name":"data","phase":"Bound","capacity":10737418240,"used":10200547328,"usedPercent":95,"measured":true}],
     "releases":[{"namespace":"shop","name":"shop","status":"pending-upgrade","revision":2,"updated":1}],"futureField":1}
    """#

    private func report() throws -> CheckupReport { try TalosJSON.decode(CheckupReport.self, from: json) }

    private func snap(_ issues: [String: String]?, watched: Bool = true, context: String = "lab") -> ClusterSnapshot {
        ClusterSnapshot(context: context, takenAt: now, nodes: ["a": NodeState(hostname: "host-a", health: .ready)],
                        etcdChecked: true, certNotAfter: farCert,
                        checkupWatched: watched, checkupChecked: watched && issues != nil, checkupIssues: issues ?? [:])
    }

    func testReportReadsAsTheScreenShowsIt() throws {
        let r = try report()
        XCTAssertEqual(r.status, .critical)
        XCTAssertEqual(r.shownSections.map(\.id), ["workloads", "events", "nodes", "storage"])
        XCTAssertEqual(r.sections[3].status, .unknown)
        XCTAssertEqual(r.count(.critical), 1)
        XCTAssertEqual(r.count(.warning), 3)
        let crash = r.sections[0].findings[0]
        XCTAssertEqual(crash.kind, .podCrashLoop)
        XCTAssertEqual(crash.subject, "shop/web-1")
        XCTAssertEqual(crash.detail, "Error (exit 2): back-off")
        // The reason of a failed pod is its title, not a detail; an unknown kind keeps its name.
        XCTAssertEqual(r.sections[0].findings[2].detail, "")
        XCTAssertNil(r.sections[0].findings[3].kind)
        XCTAssertEqual(r.sections[2].findings[0].subject, "n2")
        XCTAssertEqual(r.sections[1].section, .events)
        XCTAssertEqual(r.nodes[0].cpuPercent, 95)
        XCTAssertTrue(r.volumes[0].measured)
        XCTAssertTrue(r.releases[0].inTrouble)
    }

    func testOnlyCriticalAndWarningFindingsAlertAndEventsNever() throws {
        XCTAssertEqual(try report().alertIssues, [
            "workloads|podCrashLoop|shop/web-1": "critical",
            "workloads|podOOMKilled|shop/cache-0": "warning",
            "workloads|fromTheFuture|x": "warning",
            "nodes|nodeCordoned|n2": "warning",
        ])
    }

    func testAnUnreadSectionKeepsWhatWasKnown() throws {
        let known = ["storage|volumeFull|shop/data": "critical", "workloads|podCrashLoop|shop/gone": "critical"]
        let issues = checkupIssuesWithGaps(try report(), known: known)
        XCTAssertEqual(issues["storage|volumeFull|shop/data"], "critical")
        XCTAssertNil(issues["workloads|podCrashLoop|shop/gone"])
        XCTAssertEqual(knownCheckupIssues(snap(known, context: "other"), context: "lab"), [:])
        XCTAssertEqual(knownCheckupIssues(snap(known), context: "lab"), known)
        XCTAssertEqual(CheckupSubject(key: "storage|volumeFull|shop/data"), CheckupSubject(key: "storage|volumeFull|shop/data"))
        XCTAssertEqual(CheckupSubject(key: "nodes|nodeCordoned|n2").subject, "n2")
    }

    func testCriticalAlertsAtOnceWarningOnTheSecondCheckAndClearingOnce() {
        let crash = "workloads|podCrashLoop|shop/web-1"
        let cordon = "nodes|nodeCordoned|n2"

        // The first check is a silent baseline.
        let base = evaluate(previous: nil, current: snap([:]), now: now)
        XCTAssertTrue(base.alerts.isEmpty)

        let first = evaluate(previous: base.next, current: snap([crash: "critical", cordon: "warning"]), now: now)
        XCTAssertEqual(first.alerts.map(\.key), ["checkup:\(crash)"])
        XCTAssertEqual(first.alerts.first?.title, "workloads: shop/web-1")
        XCTAssertEqual(first.next.checkupPending, [cordon])

        let second = evaluate(previous: first.next, current: snap([crash: "critical", cordon: "warning"]), now: now)
        XCTAssertEqual(second.alerts.map(\.key), ["checkup:\(cordon)"])

        let cleared = evaluate(previous: second.next, current: snap([cordon: "warning"]), now: now)
        XCTAssertEqual(cleared.alerts.map(\.problem), [false])
        XCTAssertEqual(cleared.alerts.first?.title, "Resolved: shop/web-1")

        // Unreadable: nothing clears. Turned off: forgotten without a word.
        XCTAssertTrue(evaluate(previous: cleared.next, current: snap(nil), now: now).alerts.isEmpty)
        let off = evaluate(previous: cleared.next, current: snap([:], watched: false), now: now)
        XCTAssertTrue(off.alerts.isEmpty)
        XCTAssertTrue(off.next.checkupIssues.isEmpty)
    }

    func testEventsDecode() throws {
        let list = try TalosJSON.decode(KubeEventList.self, from: #"{"events":[{"type":"Warning","reason":"BackOff","message":"m","kind":"Pod","name":"p","count":4,"last":5},{"type":"Normal","reason":"Pulled"}]}"#)
        XCTAssertEqual(list.events.map(\.isWarning), [true, false])
        XCTAssertEqual(list.events[1].count, 1)
    }
}
