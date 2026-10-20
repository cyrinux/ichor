import XCTest
@testable import IchorCore

final class PluginFindingsTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)
    private var farCert: Int64 { Int64(now.timeIntervalSince1970) + 365 * 86_400 }

    private let weekend = "plugin:kube-green:asleep-no-wakeup:demo-lab/weekend"
    private let registry = "plugin:external-secrets:target-missing:demo-lab/registry-pull"

    private let reportJSON = #"""
    {"plugins": 2,
     "findings": [
      {"severity": "critical", "checkupNative": true, "key": "plugin:external-secrets:target-missing:demo-lab/registry-pull",
       "plugin": "external-secrets", "pluginName": "External Secrets", "pluginIcon": "external-secrets",
       "id": "target-missing", "title": "demo-lab/registry-pull", "detail": "its Secret is missing",
       "ref": {"group": "external-secrets.io", "version": "v1", "resource": "externalsecrets", "namespace": "demo-lab", "name": "registry-pull"},
       "opens": "secrets"},
      {"severity": "warning", "key": "plugin:kube-green:asleep-no-wakeup:demo-lab/weekend",
       "plugin": "kube-green", "pluginName": "kube-green", "id": "asleep-no-wakeup", "title": "demo-lab/weekend",
       "detail": "no wake-up time", "ref": {"group": "kube-green.com", "version": "v1alpha1", "resource": "sleepinfos", "namespace": "demo-lab", "name": "weekend"},
       "opens": "list"},
      {"severity": "info", "plugin": "kube-green", "pluginName": "kube-green", "id": "note", "title": "a note",
       "ref": {"resource": "sleepinfos", "name": "cluster-wide"}},
      {"severity": "warning", "plugin": "demo-widgets", "id": "loose", "title": "no object"},
      "not a finding"],
     "errors": [{"plugin": "demo-widgets", "pluginName": "Demo Widgets", "resource": "widgets.widgets.example.com",
       "message": "Kubernetes API: permission denied: forbidden"}]}
    """#

    private func report(_ json: String? = nil) throws -> PluginFindingsReport {
        try TalosJSON.decode(PluginFindingsReport.self, from: json ?? reportJSON)
    }

    private func snap(_ issues: [String: String]?, watched: Bool = true, context: String = "lab") -> ClusterSnapshot {
        ClusterSnapshot(context: context, takenAt: now, nodes: ["a": NodeState(hostname: "host-a", health: .ready)],
                        etcdChecked: true, certNotAfter: farCert,
                        pluginWatched: watched, pluginChecked: watched && issues != nil, pluginIssues: issues ?? [:])
    }

    // MARK: decoding

    func testDecodesTheReportTolerantly() throws {
        let r = try report()
        XCTAssertEqual(r.plugins, 2)
        XCTAssertEqual(r.truncated, 0)
        // The string in the list is skipped, the rest stand.
        XCTAssertEqual(r.findings.count, 4)
        XCTAssertEqual(r.findings[0].severity, .critical)
        XCTAssertTrue(r.findings[0].checkupNative)
        XCTAssertEqual(r.findings[0].info.pluginName, "External Secrets")
        XCTAssertEqual(r.findings[0].info.ref?.resource, "externalsecrets")
        XCTAssertEqual(r.findings[0].info.opens, "secrets")
        XCTAssertFalse(r.findings[1].checkupNative)
        // No key sent: Go's, rebuilt; a cluster-scoped object is named alone.
        XCTAssertEqual(r.findings[2].info.key, "plugin:kube-green:note:cluster-wide")
        XCTAssertEqual(r.findings[3].info.key, "plugin:demo-widgets:loose")
        XCTAssertEqual(r.findings[3].info.pluginLabel, "demo-widgets")
        XCTAssertEqual(r.errors.first?.resource, "widgets.widgets.example.com")
    }

    func testEmptyReport() throws {
        let r = try report("{}")
        XCTAssertEqual(r.plugins, 0)
        XCTAssertTrue(r.findings.isEmpty)
        XCTAssertTrue(r.errors.isEmpty)
    }

    func testPluginIDOfKey() {
        XCTAssertEqual(pluginIDOfFindingKey(weekend), "kube-green")
        XCTAssertEqual(pluginIDOfFindingKey("plugin:kube-green"), "kube-green")
        XCTAssertEqual(pluginIDOfFindingKey("data:longhorn|vol"), "")
        XCTAssertEqual(pluginIDOfFindingKey("plugin"), "")
    }

    // MARK: issues

    func testIssuesNotifyWarningsAndCriticalOnly() throws {
        let issues = pluginIssuesOf(try report(), checkupWatched: false)
        XCTAssertEqual(Set(issues.keys), [registry, weekend, "plugin:demo-widgets:loose"])
        let issue = PluginIssue(value: try XCTUnwrap(issues[weekend]))
        XCTAssertEqual(issue, PluginIssue(severity: dataWarning, pluginName: "kube-green", title: "demo-lab/weekend",
                                          detail: "no wake-up time", opens: "list"))
        XCTAssertEqual(pluginIssueSeverity(try XCTUnwrap(issues[registry])), dataCritical)
    }

    func testCheckupWatchedLeavesTheNativeFindingsToIt() throws {
        let issues = pluginIssuesOf(try report(), checkupWatched: true)
        XCTAssertNil(issues[registry])
        XCTAssertNotNil(issues[weekend])
    }

    func testDuplicateKeysKeepTheWorst() throws {
        let json = #"""
        {"plugins": 1, "findings": [
          {"severity": "warning", "key": "plugin:p:f:ns/a", "plugin": "p", "title": "w"},
          {"severity": "critical", "key": "plugin:p:f:ns/a", "plugin": "p", "title": "c"},
          {"severity": "warning", "key": "plugin:p:f:ns/a", "plugin": "p", "title": "w2"}]}
        """#
        let issues = pluginIssuesOf(try report(json), checkupWatched: false)
        XCTAssertEqual(issues.count, 1)
        XCTAssertEqual(PluginIssue(value: try XCTUnwrap(issues["plugin:p:f:ns/a"])).title, "c")
    }

    func testIssueValueRoundTripsAndToleratesABareSeverity() {
        let issue = PluginIssue(severity: dataCritical, pluginName: "kube-green", title: "a|b", detail: "x:y", opens: "list")
        XCTAssertEqual(PluginIssue(value: issue.value), issue)
        XCTAssertEqual(issue.value, issue.value)
        XCTAssertEqual(PluginIssue(value: "critical").severity, dataCritical)
        XCTAssertEqual(PluginIssue(value: "garbage").severity, dataWarning)
    }

    func testUnreadableKeepsThePreviousIssues() throws {
        XCTAssertNil(pluginIssuesWithGaps(nil, known: [weekend: "x"], checkupWatched: false))
        // demo-widgets could not be read in full: its known issue stays; kube-green's that is gone clears.
        let widget = "plugin:demo-widgets:other:ns/w"
        let gone = "plugin:kube-green:asleep-no-wakeup:demo-lab/gone"
        let known = [widget: PluginIssue(severity: dataWarning, pluginName: "Demo Widgets", title: "ns/w").value,
                     gone: PluginIssue(severity: dataWarning, pluginName: "kube-green", title: "demo-lab/gone").value]
        let issues = try XCTUnwrap(pluginIssuesWithGaps(try report(), known: known, checkupWatched: false))
        XCTAssertNotNil(issues[widget])
        XCTAssertNil(issues[gone])
        XCTAssertNotNil(issues[weekend])
    }

    func testTruncatedKeepsEveryKnownIssue() throws {
        let json = #"{"plugins": 1, "findings": [], "truncated": 3, "errors": []}"#
        let issues = pluginIssuesWithGaps(try report(json), known: [weekend: "v"], checkupWatched: false)
        XCTAssertEqual(issues, [weekend: "v"])
    }

    func testCheckupWatchedDropsACarriedNativeIssue() throws {
        let json = #"""
        {"plugins": 1, "findings": [{"severity": "critical", "checkupNative": true, "key": "plugin:external-secrets:f:ns/a", "plugin": "external-secrets"}],
         "truncated": 1}
        """#
        let issues = pluginIssuesWithGaps(try report(json), known: ["plugin:external-secrets:f:ns/a": "v"], checkupWatched: true)
        XCTAssertEqual(issues, [:])
    }

    func testKnownIssuesOnlyFromTheSameClusterRead() {
        let previous = snap([weekend: "v"])
        XCTAssertEqual(knownPluginIssues(previous, context: "lab"), [weekend: "v"])
        XCTAssertEqual(knownPluginIssues(previous, context: "other"), [:])
        XCTAssertEqual(knownPluginIssues(snap(nil), context: "lab"), [:])
        XCTAssertEqual(knownPluginIssues(nil, context: "lab"), [:])
    }

    // MARK: monitor

    func testSnapshotDecodesWithoutPluginFields() throws {
        let data = try JSONEncoder().encode(snap([weekend: "v"]))
        var object = try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
        for key in ["pluginWatched", "pluginChecked", "pluginIssues", "pluginPending"] { object[key] = nil }
        let decoded = try JSONDecoder().decode(ClusterSnapshot.self, from: JSONSerialization.data(withJSONObject: object))
        XCTAssertFalse(decoded.pluginWatched)
        XCTAssertEqual(decoded.pluginIssues, [:])
        let roundTrip = try JSONDecoder().decode(ClusterSnapshot.self, from: data)
        XCTAssertEqual(roundTrip.pluginIssues, [weekend: "v"])
        XCTAssertTrue(roundTrip.pluginChecked)
    }

    func testCriticalAlertsAtOnceAndClears() throws {
        let issues = pluginIssuesOf(try report(), checkupWatched: true)
        let baseline = evaluate(previous: nil, current: snap([:]), now: now)
        XCTAssertTrue(baseline.alerts.isEmpty)

        let critical = [registry: PluginIssue(severity: dataCritical, pluginName: "External Secrets", title: "demo-lab/registry-pull",
                                              detail: "its Secret is missing").value]
        let raised = evaluate(previous: baseline.next, current: snap(critical), now: now)
        XCTAssertEqual(raised.alerts, [Alert(key: registry, title: "External Secrets: demo-lab/registry-pull",
                                             text: "its Secret is missing · critical", problem: true)])
        // Seen again: quiet.
        let again = evaluate(previous: raised.next, current: snap(critical), now: now)
        XCTAssertTrue(again.alerts.isEmpty)
        // Gone: says so once, named from the issue kept.
        let cleared = evaluate(previous: again.next, current: snap([:]), now: now)
        XCTAssertEqual(cleared.alerts, [Alert(key: registry, title: "demo-lab/registry-pull is healthy again",
                                              text: "External Secrets", problem: false)])
        XCTAssertFalse(issues.isEmpty)
    }

    func testWarningAlertsOnTheSecondCheck() throws {
        let issues = pluginIssuesOf(try report(), checkupWatched: true)
        let warning = [weekend: try XCTUnwrap(issues[weekend])]
        let baseline = evaluate(previous: nil, current: snap([:]), now: now)
        let first = evaluate(previous: baseline.next, current: snap(warning), now: now)
        XCTAssertTrue(first.alerts.isEmpty)
        XCTAssertEqual(first.next.pluginPending, [weekend])
        let second = evaluate(previous: first.next, current: snap(warning), now: now)
        XCTAssertEqual(second.alerts.map(\.key), [weekend])
        XCTAssertEqual(second.alerts.first?.title, "kube-green: demo-lab/weekend")
    }

    func testUnreadCheckKeepsWhatWasKnown() {
        let critical = [registry: PluginIssue(severity: dataCritical, pluginName: "External Secrets", title: "t").value]
        let baseline = evaluate(previous: nil, current: snap(critical), now: now)
        let unread = evaluate(previous: baseline.next, current: snap(nil), now: now)
        XCTAssertTrue(unread.alerts.isEmpty)
        XCTAssertEqual(unread.next.pluginIssues, critical)
        // Watching turned off forgets them.
        let off = evaluate(previous: unread.next, current: snap(nil, watched: false), now: now)
        XCTAssertTrue(off.alerts.isEmpty)
        XCTAssertEqual(off.next.pluginIssues, [:])
    }

    func testSnapshotsCarryThePluginTrack() throws {
        let kube = kubeSnapshotOf(KubeNodesOverview(), context: "k", certNotAfter: 0, takenAt: now, pluginWatched: true, pluginIssues: [weekend: "v"])
        XCTAssertTrue(kube.pluginChecked)
        XCTAssertEqual(kube.pluginIssues, [weekend: "v"])
        let overview = try TalosJSON.decode(ClusterOverview.self, from: #"{"context":"lab","nodes":[]}"#)
        let unread = snapshotOf(overview, etcd: nil, certNotAfter: 0, takenAt: now, pluginWatched: true, pluginIssues: nil)
        XCTAssertTrue(unread.pluginWatched)
        XCTAssertFalse(unread.pluginChecked)
        let off = snapshotOf(overview, etcd: nil, certNotAfter: 0, takenAt: now, pluginWatched: false, pluginIssues: [weekend: "v"])
        XCTAssertEqual(off.pluginIssues, [:])
    }

    // MARK: links

    func testAlertOpensThePluginScreen() throws {
        let target = try XCTUnwrap(ShareTarget.forAlert(key: weekend, snapshot: snap([:])))
        XCTAssertEqual(target.target, .data)
        XCTAssertEqual(target.kind, "plugin-kube-green")
        XCTAssertEqual(target.pluginID, "kube-green")
        XCTAssertNil(target.dataServiceKind)
        XCTAssertNil(ShareTarget.forAlert(key: "plugin:", snapshot: snap([:])))
        XCTAssertNil(ShareTarget.data(nil).pluginID)
        XCTAssertNil(ShareTarget(target: .data, kind: "plugin-").pluginID)
        XCTAssertEqual(alertKind(key: weekend), "plugin")
    }

    // MARK: checkup

    private let checkupJSON = #"""
    {"status": "warning", "sections": [
      {"id": "plugins", "status": "warning", "checked": 4, "error": "demo-widgets: widgets: forbidden", "findings": [
        {"kind": "plugin", "severity": "warning", "namespace": "demo-lab", "name": "weekend",
         "key": "plugin:kube-green:schedule-warning:demo-lab/weekend",
         "plugin": "kube-green", "pluginName": "kube-green", "pluginIcon": "kube-green",
         "id": "schedule-warning", "title": "demo-lab/weekend", "detail": "no wake-up time",
         "ref": {"group": "kube-green.com", "version": "v1alpha1", "resource": "sleepinfos", "namespace": "demo-lab", "name": "weekend"},
         "opens": "list"},
        {"kind": "plugin", "severity": "critical", "namespace": "demo-lab", "name": "weekend", "plugin": "other", "id": "x", "title": "t"},
        {"kind": "plugin", "severity": "warning", "plugin": "loose", "id": "y", "title": "about nothing"},
        {"kind": "plugin", "severity": "info", "plugin": "kube-green", "id": "z", "title": "note"}]}]}
    """#

    func testCheckupDecodesPluginFindings() throws {
        let report = try TalosJSON.decode(CheckupReport.self, from: checkupJSON)
        let section = try XCTUnwrap(report.sections.first)
        XCTAssertEqual(section.section, .plugins)
        let finding = try XCTUnwrap(section.findings.first)
        XCTAssertEqual(finding.kind, .plugin)
        XCTAssertEqual(finding.plugin?.pluginName, "kube-green")
        XCTAssertEqual(finding.plugin?.title, "demo-lab/weekend")
        XCTAssertEqual(finding.plugin?.ref?.name, "weekend")
        XCTAssertEqual(finding.plugin?.opens, "list")
        XCTAssertEqual(finding.detail, "no wake-up time")
        XCTAssertEqual(finding.subject, "demo-lab/weekend")
        XCTAssertEqual(section.findings[1].plugin?.key, "plugin:other:x")
    }

    func testCheckupPluginFindingsAlertUnlessThePluginTrackIsWatched() throws {
        let report = try TalosJSON.decode(CheckupReport.self, from: checkupJSON)
        // Two findings on one object: the worst; one about no object: its title; the info: none.
        XCTAssertEqual(report.alertIssues(gitopsWatched: false), [
            "plugins|plugin|demo-lab/weekend": "critical",
            "plugins|plugin|about nothing": "warning",
        ])
        XCTAssertEqual(report.alertIssues(gitopsWatched: false, pluginsWatched: true), [:])
        // The section could not be read in full: a known plugin finding stays, unless the plugins' track has it.
        let known = ["plugins|plugin|demo-lab/gone": "warning"]
        XCTAssertEqual(checkupIssuesWithGaps(report, known: known)["plugins|plugin|demo-lab/gone"], "warning")
        XCTAssertEqual(checkupIssuesWithGaps(report, known: known, pluginsWatched: true), [:])
    }

    func testOtherKindsHaveNoPluginInfo() throws {
        let json = #"{"status":"ok","sections":[{"id":"workloads","status":"warning","findings":[{"kind":"podFailed","severity":"warning","plugin":"x","title":"t"}]}]}"#
        let report = try TalosJSON.decode(CheckupReport.self, from: json)
        XCTAssertNil(report.sections.first?.findings.first?.plugin)
    }
}
