import XCTest
@testable import IchorCore

/// Same cases as Android's AlertRulePlanTest and AlertMutesTest.
final class AlertRulePlanTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_780_000_000)

    private func alert(_ key: String, problem: Bool = true) -> Alert {
        Alert(key: key, title: "", text: "", problem: problem)
    }

    // MARK: - Kind and severity

    func testEachKindHasTheSeverityItsSnapshotKeeps() {
        let storage = StorageIssue(
            severity: dataCritical, kind: StorageIssue.fill, hostname: "worker-2", name: "EPHEMERAL")
        let trend = StorageTrendIssue(
            hostname: "worker-2", name: "EPHEMERAL", critical: false, days: 3, slopePerDay: 2, usedPercent: 80)
        let sensor = SensorIssue(severity: dataWarning, kind: SensorIssue.throttledKind, hostname: "worker-1")
        let plugin = PluginIssue(severity: dataCritical, pluginName: "kube-green", title: "weekend")
        let snapshot = ClusterSnapshot(
            context: "prod", takenAt: now, nodes: [:],
            certNotAfter: Int64(now.timeIntervalSince1970) - 2 * 86_400,
            dataIssues: ["longhorn|vol-1": dataWarning],
            gitopsIssues: ["argocd|apps/shop": "critical:syncFailed"],
            checkupIssues: ["volumes|pvc|shop/data": dataCritical],
            amIssues: ["abc": AMIssue(severity: "WARNING", alertname: "KubePodCrashLooping").value],
            storageIssues: ["10.0.0.3|EPHEMERAL": storage.value],
            storageTrends: ["10.0.0.3|EPHEMERAL": trend.value],
            sensorIssues: ["10.0.0.4|throttled": sensor.value],
            pluginIssues: ["plugin:kube-green:x:ns/weekend": plugin.value])
        let keys = [
            "node:10.0.0.2", "etcd:m1:NOSPACE", "unreachable", "am:abc", "gitops:argocd|apps/shop",
            "data:longhorn|vol-1", "checkup:volumes|pvc|shop/data", "storage:10.0.0.3|EPHEMERAL",
            "storage:10.0.0.3|EPHEMERAL:trend", "sensor:10.0.0.4|throttled", "plugin:kube-green:x:ns/weekend", "cert",
            "proposal:1", signInAlertKey,
        ]
        let got = keys.map { key in
            "\(alertRuleKind(key: key))=\(alertRuleSeverity(alert(key), snapshot: snapshot, now: now))"
        }
        XCTAssertEqual(
            got,
            [
                "node=", "etcd=", "unreachable=", "am=warning", "gitops=critical", "data=warning", "checkup=critical",
                "storage=critical", "trend=warning", "sensor=warning", "plugin=critical", "cert=critical",
                "proposal=info", "signin=warning",
            ])
    }

    func testACertificateExpiringIsAWarningAndAKubeconfigItsOwnKind() {
        let snapshot = ClusterSnapshot(
            context: "k", takenAt: now, nodes: [:], certNotAfter: Int64(now.timeIntervalSince1970) + 2 * 86_400,
            kube: true)
        XCTAssertEqual(alertRuleSeverity(alert("cert"), snapshot: snapshot, now: now), alertSeverityWarning)
        XCTAssertEqual(alertRuleKind(key: "cert", kube: true), "kubeconfig")
    }

    func testAResolvedAlertTakesItsSeverityFromThePreviousSnapshot() {
        let previous = ClusterSnapshot(
            context: "prod", takenAt: now, nodes: [:],
            amIssues: ["abc": AMIssue(severity: dataCritical, alertname: "Down").value])
        let current = ClusterSnapshot(context: "prod", takenAt: now, nodes: [:])
        XCTAssertEqual(
            alertRuleSeverity(alert("am:abc", problem: false), snapshot: current, previous: previous, now: now),
            alertSeverityCritical)
    }

    // MARK: - Plan

    private let inputs = [
        AlertInput(cluster: "fp-prod", key: "node:10.0.0.2", kind: "node", problem: true),
        AlertInput(cluster: "fp-prod", key: "am:abc", kind: "am", severity: "warning", problem: true),
        AlertInput(cluster: "fp-prod", key: "data:x", kind: "data", severity: "warning", problem: true, snoozed: true),
        AlertInput(cluster: "fp-staging", key: "gitops:a", kind: "gitops", severity: "critical", problem: true),
        AlertInput(cluster: "fp-staging", key: "am:def", kind: "am", severity: "info", problem: true),
    ]

    func testWithoutDecisionsEveryUnsnoozedAlertIsPosted() {
        let plan = alertRulePlan(inputs: inputs, decisions: nil)
        XCTAssertEqual(plan.outcomes, [.notify, .notify, .drop, .notify, .notify])
        XCTAssertTrue(plan.digests.isEmpty)
    }

    func testNotifyMuteAndDigestAreSplit() {
        let digest = AlertDigest(cluster: "fp-staging", count: 2, critical: 1, firstKeys: ["gitops:a", "am:def"])
        let decisions = AlertDecisions(
            decisions: [
                AlertDecision(cluster: "fp-prod", key: "node:10.0.0.2", action: alertActionNotify),
                AlertDecision(
                    cluster: "fp-prod", key: "am:abc", action: alertActionMute, reason: alertReasonQuiet, until: 7_000),
                AlertDecision(cluster: "fp-prod", key: "data:x", action: alertActionMute, reason: alertReasonSnoozed),
                AlertDecision(
                    cluster: "fp-staging", key: "gitops:a", action: alertActionDigest, reason: alertReasonDigest),
                AlertDecision(
                    cluster: "fp-staging", key: "am:def", action: alertActionDigest, reason: alertReasonDigest),
            ],
            digests: [digest])
        let plan = alertRulePlan(inputs: inputs, decisions: decisions)
        // The snoozed one is not recorded: the user silenced it themselves.
        XCTAssertEqual(plan.outcomes, [.notify, .mute(reason: alertReasonQuiet, until: 7_000), .drop, .digest, .digest])
        XCTAssertEqual(plan.digests, [digest])
    }

    func testASeverityMuteIsRecorded() {
        let input = [AlertInput(cluster: "fp", key: "am:abc", kind: "am", severity: "warning", problem: true)]
        let decisions = AlertDecisions(decisions: [
            AlertDecision(cluster: "fp", key: "am:abc", action: alertActionMute, reason: alertReasonSeverity)
        ])
        XCTAssertEqual(
            alertRulePlan(inputs: input, decisions: decisions).outcomes, [.mute(reason: alertReasonSeverity, until: 0)])
    }

    func testAnAlertWithoutAMatchingDecisionIsPosted() {
        // Go answered for another key at the second place, and not at all after it.
        let decisions = AlertDecisions(decisions: [
            AlertDecision(cluster: "fp-prod", key: "node:10.0.0.2", action: alertActionMute, reason: alertReasonQuiet),
            AlertDecision(cluster: "fp-prod", key: "am:other", action: alertActionMute),
        ])
        let plan = alertRulePlan(inputs: inputs, decisions: decisions)
        XCTAssertEqual(plan.outcomes, [.mute(reason: alertReasonQuiet, until: 0), .notify, .drop, .notify, .notify])
    }

    func testADigestGoDidNotCountIsPostedOnItsOwn() {
        let input = [AlertInput(cluster: "fp-staging", key: "gitops:a", kind: "gitops", problem: true)]
        let decisions = AlertDecisions(decisions: [
            AlertDecision(cluster: "fp-staging", key: "gitops:a", action: alertActionDigest, reason: alertReasonDigest)
        ])
        let plan = alertRulePlan(inputs: input, decisions: decisions)
        XCTAssertEqual(plan.outcomes, [.notify])
        XCTAssertTrue(plan.digests.isEmpty)
    }

    func testOneDigestPerClusterAcrossItsGroups() {
        // A run's alerts, then its pull requests: the same cluster twice, not in a row.
        let input = [
            AlertInput(cluster: "fp-staging", key: "am:abc", kind: "am", severity: "warning", problem: true),
            AlertInput(cluster: "fp-prod", key: "node:1", kind: "node", problem: true),
            AlertInput(cluster: "fp-staging", key: "proposal:1", kind: "proposal", severity: "info", problem: false),
        ]
        let digest = AlertDigest(cluster: "fp-staging", count: 2, warning: 1)
        let decisions = AlertDecisions(
            decisions: [
                AlertDecision(cluster: "fp-staging", key: "am:abc", action: alertActionDigest),
                AlertDecision(cluster: "fp-prod", key: "node:1", action: alertActionNotify),
                AlertDecision(cluster: "fp-staging", key: "proposal:1", action: alertActionDigest),
            ],
            digests: [digest])
        let plan = alertRulePlan(inputs: input, decisions: decisions)
        XCTAssertEqual(plan.outcomes, [.digest, .notify, .digest])
        XCTAssertEqual(plan.digests, [digest])
    }

    func testTheHistoryNamesAlertsLikeTheirNotifications() {
        XCTAssertEqual(historyAlertKey(track: HistoryTrack.alertmanager, key: "abc"), "am:abc")
        XCTAssertEqual(historyAlertKey(track: HistoryTrack.etcd, key: "m1:NOSPACE"), "etcd:m1:NOSPACE")
        XCTAssertEqual(historyAlertKey(track: HistoryTrack.cert, key: "cert"), "cert")
    }

    // MARK: - Mutes

    private let quiet = AlertMute(reason: alertReasonQuiet, until: 7_000, at: 1_000)

    func testTheLatestMuteOfAKeyIsKept() {
        var mutes = AlertMutes().recording(cluster: "c", key: "k", quiet)
        XCTAssertEqual(mutes.of(cluster: "c", key: "k"), quiet)
        let later = AlertMute(reason: alertReasonSeverity, until: 0, at: 2_000)
        mutes = mutes.recording(cluster: "c", key: "k", later)
        XCTAssertEqual(mutes.of(cluster: "c", key: "k"), later)
        XCTAssertNil(mutes.of(cluster: "other", key: "k"))
    }

    func testAMuteShowsOnTheAlertOpenAtThatTime() {
        let mutes = AlertMutes().recording(cluster: "c", key: "am:x", quiet)
        XCTAssertEqual(mutes.during(cluster: "c", key: "am:x", from: 1_000, to: nil), quiet)
        XCTAssertEqual(mutes.during(cluster: "c", key: "am:x", from: 500, to: 1_500), quiet)
        // Opened again after it: that one was not muted.
        XCTAssertNil(mutes.during(cluster: "c", key: "am:x", from: 1_001, to: nil))
        XCTAssertNil(mutes.during(cluster: "c", key: "am:x", from: 0, to: 999))
    }

    func testOldMutesAndRemovedClustersGo() {
        let fresh = AlertMute(reason: alertReasonQuiet, until: 0, at: AlertMutes.keepMillis + 5_000)
        let mutes = AlertMutes()
            .recording(cluster: "c", key: "old", quiet)
            .recording(cluster: "c", key: "new", fresh)
            .recording(cluster: "gone", key: "new", fresh)
            .pruned(now: AlertMutes.keepMillis + 6_000)
        XCTAssertNil(mutes.of(cluster: "c", key: "old"))
        let kept = mutes.keeping(clusters: ["c"])
        XCTAssertNil(kept.of(cluster: "gone", key: "new"))
        XCTAssertEqual(kept.entries, ["c": ["new": fresh]])
    }

    func testBlankClustersOrKeysAreNotStored() {
        let mutes = AlertMutes().recording(cluster: "", key: "k", quiet).recording(cluster: "c", key: "", quiet)
        XCTAssertTrue(mutes.entries.isEmpty)
    }
}
