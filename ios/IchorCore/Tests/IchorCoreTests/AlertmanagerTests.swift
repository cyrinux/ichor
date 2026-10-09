import XCTest
@testable import IchorCore

final class AlertmanagerTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)
    private var farCert: Int64 { Int64(now.timeIntervalSince1970) + 365 * 86_400 }

    /// The Go demo's answer, trimmed (alertmanager_demo.go).
    private let demo = #"""
    {"groups":[
      {"alertname":"NodeFilesystemAlmostOutOfSpace","severity":"critical","count":1,"active":1,"alerts":[
        {"fingerprint":"a1c3e5f7a9b1c3d5","alertname":"NodeFilesystemAlmostOutOfSpace","severity":"critical",
         "labels":{"alertname":"NodeFilesystemAlmostOutOfSpace","severity":"critical","instance":"demo-worker-1","mountpoint":"/var","namespace":"monitoring"},
         "annotations":{"summary":"Filesystem has less than 5% space left.","runbook_url":"https://runbooks.example.invalid/x","team":"platform"},
         "summary":"Filesystem has less than 5% space left.","runbookURL":"https://runbooks.example.invalid/x",
         "generatorURL":"http://prometheus.demo.invalid/graph","startsAt":1791540000000,"endsAt":1791543060000,"updatedAt":1791542790000,
         "receivers":["oncall-pager","team-chat"],"state":"active","silencedBy":[],"inhibitedBy":[]}]},
      {"alertname":"KubePodCrashLooping","severity":"warning","count":1,"active":1,"alerts":[
        {"fingerprint":"b2d4f6a8c0e2a4b6","alertname":"KubePodCrashLooping","severity":"warning",
         "labels":{"alertname":"KubePodCrashLooping","severity":"warning","namespace":"demo","pod":"worker-6f4b8-uvwxy"},
         "annotations":{},"receivers":["team-chat"],"state":"active","silencedBy":[],"inhibitedBy":[]}]},
      {"alertname":"Watchdog","severity":"info","count":1,"active":1,"alerts":[
        {"fingerprint":"c3e5a7b9d1f3c5e7","alertname":"Watchdog","severity":"info","labels":{"alertname":"Watchdog"},
         "state":"active"}]},
      {"alertname":"CPUThrottlingHigh","severity":"info","count":1,"active":0,"alerts":[
        {"fingerprint":"d4f6b8c0e2a4d6f8","alertname":"CPUThrottlingHigh","severity":"warning","labels":{"namespace":"kube-system","pod":"coredns-x"},
         "state":"suppressed","silencedBy":["5b0d7c3e-9a41-4f2e-8c6d-1e2f3a4b5c6d"],"inhibitedBy":[]}]},
      {"alertname":"Unlabelled","severity":"brand-new","count":1,"active":1,"alerts":[
        {"fingerprint":"e5a7c9d1f3b5e7a9","alertname":"Unlabelled","severity":"brand-new","labels":{"job":"api"},"state":"unprocessed"}]}],
     "counts":{"critical":1,"warning":1,"info":1,"other":1,"suppressed":1},"total":5,"truncated":false}
    """#

    private func decoded() throws -> AMAlerts { try TalosJSON.decode(AMAlerts.self, from: demo) }

    // MARK: models

    func testDecodesTheGoShape() throws {
        let alerts = try decoded()
        XCTAssertEqual(alerts.groups.map(\.alertname).first, "NodeFilesystemAlmostOutOfSpace")
        XCTAssertEqual(alerts.counts.firing, 4)
        XCTAssertEqual(alerts.counts.count(.critical), 1)
        XCTAssertEqual(alerts.counts.suppressed, 1)
        XCTAssertEqual(alerts.total, 5)
        let first = try XCTUnwrap(alerts.alerts.first)
        XCTAssertEqual(first.severity, .critical)
        XCTAssertEqual(first.receivers, ["oncall-pager", "team-chat"])
        XCTAssertEqual(first.runbookURL, "https://runbooks.example.invalid/x")
        XCTAssertEqual(first.otherAnnotations.map(\.key), ["team"])
        XCTAssertEqual(first.subject, "demo-worker-1")
        // Unknown severities read as other; fields left out read as empty.
        let odd = try XCTUnwrap(alerts.alerts.last)
        XCTAssertEqual(odd.severity, .other)
        XCTAssertEqual(odd.state, .unprocessed)
        XCTAssertTrue(odd.receivers.isEmpty)
        XCTAssertEqual(odd.subject, "api")
        let silenced = try XCTUnwrap(alerts.alerts.first { $0.alertname == "CPUThrottlingHigh" })
        XCTAssertTrue(silenced.suppressed && silenced.silenced && !silenced.inhibited)
        XCTAssertEqual(silenced.subject, "kube-system/coredns-x")
        XCTAssertEqual(try TalosJSON.decode(AMAlerts.self, from: "{}"), AMAlerts())
    }

    func testFilteringBySeverityAndSearch() throws {
        let alerts = try decoded()
        XCTAssertEqual(alerts.filtered(severities: Set(AMSeverity.allCases), query: "").count, 5)
        XCTAssertEqual(alerts.filtered(severities: [.critical, .warning], query: "").map(\.alertname),
                       ["NodeFilesystemAlmostOutOfSpace", "KubePodCrashLooping", "CPUThrottlingHigh"])
        // The group's severity follows the alerts kept.
        XCTAssertEqual(alerts.filtered(severities: [.warning], query: "").last?.severity, .warning)
        // Search reads the name, the summary and the labels.
        XCTAssertEqual(alerts.filtered(severities: Set(AMSeverity.allCases), query: "worker-6F4B8").map(\.alertname), ["KubePodCrashLooping"])
        XCTAssertEqual(alerts.filtered(severities: Set(AMSeverity.allCases), query: "5% space").map(\.alertname),
                       ["NodeFilesystemAlmostOutOfSpace"])
        XCTAssertEqual(alerts.filtered(severities: Set(AMSeverity.allCases), query: "mountpoint").count, 1)
        XCTAssertTrue(alerts.filtered(severities: [], query: "").isEmpty)
    }

    func testSilencesDecode() throws {
        let json = #"""
        {"silences":[{"id":"5b0d7c3e-9a41-4f2e-8c6d-1e2f3a4b5c6d","state":"active",
          "matchers":[{"name":"alertname","value":"CPUThrottlingHigh","isRegex":false,"isEqual":true},
                      {"name":"pod","value":"worker-.*","isRegex":true,"isEqual":false}],
          "createdBy":"ichor","comment":"limits are being raised","startsAt":1791470000000,"endsAt":1791642800000},
          {"id":"old","state":"expired","matchers":[{"name":"job","value":"x"}]}]}
        """#
        let silences = try TalosJSON.decode(AMSilences.self, from: json).silences
        XCTAssertEqual(silences.count, 2)
        XCTAssertTrue(silences[0].expirable)
        XCTAssertFalse(silences[1].expirable)
        XCTAssertEqual(silences[0].matchers.map(\.text), [#"alertname="CPUThrottlingHigh""#, #"pod!~"worker-.*""#])
        // isEqual left out reads as true.
        XCTAssertEqual(silences[1].matchers.first?.op, "=")
    }

    func testMatcherOperatorsAndEncoding() throws {
        XCTAssertEqual(AMMatcher(name: "a", value: "b").op, "=")
        XCTAssertEqual(AMMatcher(name: "a", value: "b", isEqual: false).op, "!=")
        XCTAssertEqual(AMMatcher(name: "a", value: "b", isRegex: true).op, "=~")
        XCTAssertEqual(AMMatcher(name: "a", value: "b", isRegex: true, isEqual: false).op, "!~")
        let json = String(decoding: try JSONEncoder().encode([AMMatcher(name: "pod", value: "x")]), as: UTF8.self)
        XCTAssertEqual(try TalosJSON.decode([AMMatcher].self, from: json), [AMMatcher(name: "pod", value: "x")])
    }

    func testCustomDurations() {
        XCTAssertEqual(amParseDuration("90m"), 90)
        XCTAssertEqual(amParseDuration("6h"), 360)
        XCTAssertEqual(amParseDuration(" 2d "), 2880)
        XCTAssertEqual(amParseDuration("1w"), 10_080)
        XCTAssertEqual(amParseDuration("45"), 45)
        XCTAssertEqual(amParseDuration("30d"), 43_200)
        XCTAssertNil(amParseDuration("31d"))
        XCTAssertNil(amParseDuration("5w"))
        XCTAssertNil(amParseDuration("0h"))
        XCTAssertNil(amParseDuration("soon"))
        XCTAssertNil(amParseDuration(""))
        XCTAssertEqual(amSilenceDurations, [60, 240, 1440, 10_080])
    }

    func testTheNodeAnAlertIsAbout() {
        let nodes = [NodeOverview(node: "10.0.0.5", hostname: "demo-worker-1", reachable: true),
                     NodeOverview(node: "fd00::7", hostname: "demo-cp-1", reachable: true)]
        XCTAssertEqual(alertNode(["instance": "demo-worker-1"], in: nodes)?.node, "10.0.0.5")
        XCTAssertEqual(alertNode(["instance": "10.0.0.5:9100"], in: nodes)?.hostname, "demo-worker-1")
        XCTAssertEqual(alertNode(["instance": "[fd00::7]:9100"], in: nodes)?.hostname, "demo-cp-1")
        XCTAssertEqual(alertNode(["node": "demo-cp-1", "instance": "10.0.0.5:9100"], in: nodes)?.hostname, "demo-cp-1")
        XCTAssertNil(alertNode(["instance": "10.9.9.9:9100"], in: nodes))
        XCTAssertNil(alertNode(["pod": "x"], in: nodes))
        XCTAssertEqual(amHost("fd00::7"), "fd00::7")
    }

    // MARK: monitor

    private func snap(_ issues: [String: String]?, watched: Bool = true, context: String = "lab") -> ClusterSnapshot {
        ClusterSnapshot(context: context, takenAt: now, nodes: ["a": NodeState(hostname: "host-a", health: .ready)],
                        etcdChecked: true, certNotAfter: farCert,
                        amWatched: watched, amChecked: watched && issues != nil, amIssues: issues ?? [:])
    }

    private let disk = "a1c3e5f7a9b1c3d5"
    private var diskValue: String {
        AMIssue(severity: dataCritical, alertname: "NodeFilesystemAlmostOutOfSpace", subject: "demo-worker-1",
                summary: "Filesystem has less than 5% space left.").value
    }
    private let crash = "b2d4f6a8c0e2a4b6"
    private var crashValue: String {
        AMIssue(severity: dataWarning, alertname: "KubePodCrashLooping", subject: "demo/worker-6f4b8-uvwxy").value
    }

    func testIssuesSkipSuppressedAndInfo() throws {
        let issues = alertmanagerIssuesOf(try decoded())
        XCTAssertEqual(Set(issues.keys), [disk, crash, "e5a7c9d1f3b5e7a9"])
        XCTAssertEqual(issues[disk], diskValue)
        XCTAssertEqual(issues[crash], crashValue)
        // No known severity: a warning.
        XCTAssertEqual(AMIssue(value: issues["e5a7c9d1f3b5e7a9"] ?? "").severity, dataWarning)
    }

    func testATruncatedAnswerKeepsKnownAlertsItLacks() throws {
        let gone = "f6b8d0e2a4c6f8b0"
        let goneValue = AMIssue(severity: dataCritical, alertname: "KubeNodeNotReady", subject: "demo-worker-2").value
        let known = [gone: goneValue, disk: diskValue, "d4f6b8c0e2a4d6f8": crashValue]
        let full = try decoded()
        let cut = AMAlerts(groups: full.groups, counts: full.counts, total: 1500, truncated: true)
        let issues = alertmanagerIssuesWithGaps(cut, known: known)
        // Missing from the first 1000: kept. In the answer but now silenced: no longer an issue.
        XCTAssertEqual(issues[gone], goneValue)
        XCTAssertNil(issues["d4f6b8c0e2a4d6f8"])
        XCTAssertEqual(issues[crash], crashValue)
        // Not truncated: exactly what was read, the missing one resolves.
        XCTAssertNil(alertmanagerIssuesWithGaps(full, known: known)[gone])

        // Through the evaluation: no "resolved" for the one cut off, a new critical still alerts.
        let previous = snap([gone: goneValue])
        let result = evaluate(previous: previous, current: snap(issues), now: now)
        XCTAssertEqual(result.alerts.map(\.key), ["am:\(disk)"])
        XCTAssertEqual(result.next.amIssues[gone], goneValue)
    }

    func testKnownIssuesOnlyFromTheSameWatchedCluster() {
        let issues = [disk: diskValue]
        XCTAssertEqual(knownAlertmanagerIssues(snap(issues), context: "lab"), issues)
        XCTAssertEqual(knownAlertmanagerIssues(snap(issues, context: "other"), context: "lab"), [:])
        XCTAssertEqual(knownAlertmanagerIssues(snap(nil), context: "lab"), [:])
        XCTAssertEqual(knownAlertmanagerIssues(snap(issues, watched: false), context: "lab"), [:])
        XCTAssertEqual(knownAlertmanagerIssues(nil, context: "lab"), [:])
    }

    func testIssueValueRoundTrips() {
        let issue = AMIssue(severity: dataCritical, alertname: "A|B", subject: "ns/pod", summary: "x | y")
        let back = AMIssue(value: issue.value)
        XCTAssertEqual(back.alertname, "A/B")
        XCTAssertEqual(back.summary, "x | y")
        XCTAssertEqual(back.line(severity: "critical"), "ns/pod · critical")
        XCTAssertEqual(AMIssue(value: "").severity, dataWarning)
        XCTAssertEqual(AMIssue(severity: dataWarning, alertname: "W").line(severity: "warning"), "warning")
    }

    func testFirstCheckIsASilentBaseline() {
        let result = evaluate(previous: nil, current: snap([disk: diskValue]), now: now)
        XCTAssertTrue(result.alerts.isEmpty)
        XCTAssertEqual(result.next.amIssues, [disk: diskValue])
    }

    func testCriticalAlertsAtOnce() throws {
        let result = evaluate(previous: snap([:]), current: snap([disk: diskValue]), now: now)
        let alert = try XCTUnwrap(result.alerts.first)
        XCTAssertEqual(result.alerts.count, 1)
        XCTAssertEqual(alert.key, "am:\(disk)")
        XCTAssertEqual(alert.title, "Alertmanager: NodeFilesystemAlmostOutOfSpace")
        XCTAssertEqual(alert.text, "demo-worker-1 · critical\nFilesystem has less than 5% space left.")
        XCTAssertTrue(alert.problem)
    }

    func testWarningNeedsTwoChecksInARow() {
        let first = evaluate(previous: snap([:]), current: snap([crash: crashValue]), now: now)
        XCTAssertTrue(first.alerts.isEmpty)
        XCTAssertEqual(first.next.amPending, [crash])
        let second = evaluate(previous: first.next, current: snap([crash: crashValue]), now: now)
        XCTAssertEqual(second.alerts.map(\.title), ["Alertmanager: KubePodCrashLooping"])
        XCTAssertTrue(evaluate(previous: second.next, current: snap([crash: crashValue]), now: now).alerts.isEmpty)
        // Seen once then gone: never alerts.
        XCTAssertTrue(evaluate(previous: first.next, current: snap([:]), now: now).alerts.isEmpty)
    }

    func testResolvedSaysSoOnceWithItsName() {
        let result = evaluate(previous: snap([disk: diskValue]), current: snap([:]), now: now)
        XCTAssertEqual(result.alerts.map(\.title), ["Alertmanager: NodeFilesystemAlmostOutOfSpace resolved"])
        XCTAssertEqual(result.alerts.map(\.text), ["demo-worker-1"])
        XCTAssertEqual(result.alerts.map(\.problem), [false])
        XCTAssertTrue(evaluate(previous: result.next, current: snap([:]), now: now).alerts.isEmpty)
    }

    func testAnUnreadableCheckKeepsWhatWasKnown() {
        var known = snap([disk: diskValue])
        known.amPending = [crash]
        let result = evaluate(previous: known, current: snap(nil), now: now)
        XCTAssertTrue(result.alerts.isEmpty)
        XCTAssertEqual(result.next.amIssues, known.amIssues)
        XCTAssertEqual(result.next.amPending, [crash])
        XCTAssertTrue(result.next.amChecked)
    }

    func testOffForgetsAndAnotherClusterIsABaseline() {
        let off = evaluate(previous: snap([disk: diskValue]), current: snap(nil, watched: false), now: now)
        XCTAssertTrue(off.alerts.isEmpty)
        XCTAssertTrue(off.next.amIssues.isEmpty)
        XCTAssertTrue(evaluate(previous: off.next, current: snap([disk: diskValue]), now: now).alerts.isEmpty)
        XCTAssertTrue(evaluate(previous: snap([:], context: "other"), current: snap([disk: diskValue]), now: now).alerts.isEmpty)
    }

    func testSnapshotsCarryTheTrack() throws {
        let overview = try TalosJSON.decode(ClusterOverview.self, from: #"{"context":"lab","nodes":[]}"#)
        let unread = snapshotOf(overview, etcd: nil, certNotAfter: 0, takenAt: now, alertmanagerWatched: true, alertmanagerIssues: nil)
        XCTAssertTrue(unread.amWatched)
        XCTAssertFalse(unread.amChecked)
        let read = kubeSnapshotOf(KubeNodesOverview(), context: "lab", certNotAfter: 0, takenAt: now,
                                  alertmanagerWatched: true, alertmanagerIssues: [disk: diskValue])
        XCTAssertTrue(read.amChecked)
        XCTAssertEqual(read.amIssues, [disk: diskValue])
        let off = snapshotOf(overview, etcd: nil, certNotAfter: 0, takenAt: now, alertmanagerWatched: false, alertmanagerIssues: [disk: diskValue])
        XCTAssertTrue(off.amIssues.isEmpty)
        // Saved by an older version: no Alertmanager fields.
        let old = #"{"context":"lab","takenAt":0,"nodes":{}}"#
        let decoded = try JSONDecoder().decode(ClusterSnapshot.self, from: Data(old.utf8))
        XCTAssertFalse(decoded.amWatched)
        XCTAssertTrue(decoded.amIssues.isEmpty)
        let roundTrip = try JSONDecoder().decode(ClusterSnapshot.self, from: JSONEncoder().encode(snap([disk: diskValue])))
        XCTAssertEqual(roundTrip.amIssues, [disk: diskValue])
    }
}
