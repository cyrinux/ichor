import XCTest
@testable import IchorCore

final class GitOpsAlertsTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)
    private var farCert: Int64 { Int64(now.timeIntervalSince1970) + 365 * 86_400 }

    private func snap(_ issues: [String: String]?, watched: Bool = true, context: String = "lab",
                      data: [String: String] = [:]) -> ClusterSnapshot {
        ClusterSnapshot(context: context, takenAt: now, nodes: ["a": NodeState(hostname: "host-a", health: .ready)],
                        etcdChecked: true, certNotAfter: farCert,
                        dataWatched: true, dataChecked: true, dataIssues: data,
                        gitopsWatched: watched, gitopsChecked: watched && issues != nil, gitopsIssues: issues ?? [:])
    }

    private let failed = "argocd|argocd/grafana"
    private let failedValue = "critical:syncFailed"
    private let drift = "argocd|argocd/cert-manager"
    private let driftValue = "warning:outOfSync"

    // MARK: issues

    func testArgoIssues() throws {
        let json = #"""
        {"installed":true,"apps":[
          {"namespace":"argocd","name":"grafana","health":"Healthy","sync":"Synced","operation":{"phase":"Failed"}},
          {"namespace":"argocd","name":"errored","health":"Healthy","sync":"Synced","operation":{"phase":"Error"}},
          {"namespace":"argocd","name":"demo-worker","health":"Degraded","sync":"Synced","operation":{"phase":"Succeeded"}},
          {"namespace":"argocd","name":"gone","health":"Missing","sync":"OutOfSync"},
          {"namespace":"argocd","name":"cert-manager","health":"Healthy","sync":"OutOfSync","autoSync":{"enabled":true}},
          {"namespace":"argocd","name":"manual","health":"Healthy","sync":"OutOfSync","autoSync":{"enabled":false}},
          {"namespace":"argocd","name":"broken-spec","health":"Healthy","sync":"Unknown","conditions":[{"type":"ComparisonError","message":"x"}]},
          {"namespace":"argocd","name":"invalid","health":"Healthy","sync":"Synced","conditions":[{"type":"InvalidSpecError"}]},
          {"namespace":"argocd","name":"sync-error","health":"Healthy","sync":"Synced","conditions":[{"type":"SyncError"}]},
          {"namespace":"argocd","name":"orphans","health":"Healthy","sync":"Synced","conditions":[{"type":"OrphanedResourceWarning"}]},
          {"namespace":"argocd","name":"rolling","health":"Progressing","sync":"Synced","operation":{"phase":"Running"}},
          {"namespace":"argocd","name":"paused","health":"Suspended","sync":"Synced"},
          {"namespace":"argocd","name":"fine","health":"Healthy","sync":"Synced","autoSync":{"enabled":true}}]}
        """#
        let argo = try TalosJSON.decode(ArgoStatus.self, from: json)
        XCTAssertEqual(gitopsIssuesOf(argo: argo, flux: nil), [
            "argocd|argocd/grafana": "critical:syncFailed",
            "argocd|argocd/errored": "critical:syncFailed",
            "argocd|argocd/demo-worker": "critical:degraded",
            "argocd|argocd/gone": "critical:missing",
            "argocd|argocd/cert-manager": "warning:outOfSync",
            "argocd|argocd/broken-spec": "warning:error",
            "argocd|argocd/invalid": "warning:error",
            "argocd|argocd/sync-error": "warning:error",
        ])
    }

    func testFluxIssues() throws {
        let json = #"""
        {"installed":true,"apps":[
          {"kind":"HelmRelease","namespace":"ingress","name":"ingress-nginx","level":"critical"},
          {"kind":"Kustomization","namespace":"flux-system","name":"apps","level":"critical"},
          {"kind":"Kustomization","namespace":"flux-system","name":"held","level":"critical","suspended":true},
          {"kind":"HelmRelease","namespace":"monitoring","name":"loki","level":"warning","reconciling":true},
          {"kind":"Kustomization","namespace":"flux-system","name":"infra","level":"ok"},
          {"kind":"HelmRelease","namespace":"x","name":"off","level":"idle","suspended":true}]}
        """#
        let flux = try TalosJSON.decode(FluxStatus.self, from: json)
        XCTAssertEqual(gitopsIssuesOf(argo: nil, flux: flux), [
            "flux|HelmRelease ingress/ingress-nginx": "critical:notReady",
            "flux|Kustomization flux-system/apps": "critical:notReady",
        ])
    }

    func testToolsNotInstalledAddNothing() {
        XCTAssertTrue(gitopsIssuesOf(argo: ArgoStatus(installed: false), flux: FluxStatus(installed: false)).isEmpty)
        XCTAssertTrue(gitopsIssuesOf(argo: nil, flux: nil).isEmpty)
    }

    func testAPartialReadKeepsWhatWasKnownOfTheUnreadParts() throws {
        let known = ["argocd|argocd/grafana": "critical:syncFailed",
                     "flux|HelmRelease ingress/ingress-nginx": "critical:notReady",
                     "flux|Kustomization flux-system/apps": "critical:notReady"]
        let flux = try TalosJSON.decode(FluxStatus.self, from: #"""
            {"installed":true,"helmError":"forbidden","apps":[
              {"kind":"Kustomization","namespace":"flux-system","name":"apps","level":"ok"},
              {"kind":"Kustomization","namespace":"flux-system","name":"infra","level":"critical"}]}
            """#)
        XCTAssertEqual(gitopsIssuesWithGaps(argo: nil, flux: flux, known: known), [
            "argocd|argocd/grafana": "critical:syncFailed",
            "flux|HelmRelease ingress/ingress-nginx": "critical:notReady",
            "flux|Kustomization flux-system/infra": "critical:notReady",
        ])
        XCTAssertNil(gitopsIssuesWithGaps(argo: nil, flux: nil, known: known))
        // Both read: exactly what was read.
        XCTAssertEqual(gitopsIssuesWithGaps(argo: ArgoStatus(), flux: FluxStatus(), known: known), [:])
        // Flux unread: its known issues stay, Argo CD's are re-read.
        XCTAssertEqual(gitopsIssuesWithGaps(argo: ArgoStatus(), flux: nil, known: known), [
            "flux|HelmRelease ingress/ingress-nginx": "critical:notReady",
            "flux|Kustomization flux-system/apps": "critical:notReady",
        ])
    }

    func testSubjectOfKeys() {
        let argo = GitOpsSubject(key: "argocd|argocd/grafana")
        XCTAssertEqual(argo.tool, "argocd")
        XCTAssertEqual(argo.kind, "")
        XCTAssertEqual(argo.label, "argocd/grafana")
        XCTAssertEqual(argo.title, "grafana")
        let flux = GitOpsSubject(key: "flux|HelmRelease ingress/ingress-nginx")
        XCTAssertTrue(flux.isFlux)
        XCTAssertEqual(flux.kind, "HelmRelease")
        XCTAssertEqual(flux.namespace, "ingress")
        XCTAssertEqual(flux.name, "ingress-nginx")
        XCTAssertEqual(flux.title, "HelmRelease ingress-nginx")
        XCTAssertEqual(gitopsSeverity("warning:outOfSync"), dataWarning)
        XCTAssertEqual(gitopsReason("critical:degraded"), .degraded)
    }

    // MARK: evaluation

    func testFirstCheckIsASilentBaseline() {
        let result = evaluate(previous: nil, current: snap([failed: failedValue]), now: now)
        XCTAssertTrue(result.alerts.isEmpty)
        XCTAssertEqual(result.next.gitopsIssues, [failed: failedValue])
    }

    func testCriticalAlertsAtOnceWordedByReason() throws {
        let result = evaluate(previous: snap([:]), current: snap([failed: failedValue, "argocd|argocd/demo-worker": "critical:degraded",
                                                                  "flux|HelmRelease ingress/ingress-nginx": "critical:notReady"]), now: now)
        XCTAssertEqual(result.alerts.map(\.key), ["gitops:argocd|argocd/demo-worker", "gitops:argocd|argocd/grafana",
                                                  "gitops:flux|HelmRelease ingress/ingress-nginx"])
        XCTAssertEqual(result.alerts.map(\.title), ["Argo CD: demo-worker is degraded", "Argo CD: grafana sync failed",
                                                    "Flux: HelmRelease ingress-nginx is not ready"])
        let alert = try XCTUnwrap(result.alerts.first)
        XCTAssertEqual(alert.text, "argocd/demo-worker · critical")
        XCTAssertTrue(alert.problem)
    }

    func testWarningNeedsTwoChecksInARow() {
        let first = evaluate(previous: snap([:]), current: snap([drift: driftValue]), now: now)
        XCTAssertTrue(first.alerts.isEmpty)
        XCTAssertEqual(first.next.gitopsPending, [drift])

        let second = evaluate(previous: first.next, current: snap([drift: driftValue]), now: now)
        XCTAssertEqual(second.alerts.map(\.title), ["Argo CD: cert-manager is out of sync"])
        XCTAssertTrue(second.next.gitopsPending.isEmpty)

        XCTAssertTrue(evaluate(previous: second.next, current: snap([drift: driftValue]), now: now).alerts.isEmpty)
    }

    func testAShortDriftNeverAlerts() {
        let first = evaluate(previous: snap([:]), current: snap([drift: driftValue]), now: now)
        let synced = evaluate(previous: first.next, current: snap([:]), now: now)
        XCTAssertTrue(synced.alerts.isEmpty)
        XCTAssertTrue(synced.next.gitopsPending.isEmpty)
    }

    func testEscalationAlertsAgainButDeescalationDoesNot() {
        var warned = snap([:])
        warned.gitopsIssues = [drift: driftValue]
        let worse = evaluate(previous: warned, current: snap([drift: "critical:degraded"]), now: now)
        XCTAssertEqual(worse.alerts.map(\.title), ["Argo CD: cert-manager is degraded"])
        XCTAssertTrue(evaluate(previous: worse.next, current: snap([drift: driftValue]), now: now).alerts.isEmpty)
    }

    func testClearingIsNotifiedOnce() {
        var known = snap([:])
        known.gitopsIssues = [failed: failedValue, "flux|HelmRelease ingress/ingress-nginx": "critical:notReady"]
        let result = evaluate(previous: known, current: snap([:]), now: now)
        XCTAssertEqual(result.alerts.map(\.title), ["Argo CD: grafana is synced and healthy again",
                                                    "Flux: HelmRelease ingress-nginx is ready again"])
        XCTAssertEqual(result.alerts.map(\.problem), [false, false])
        XCTAssertEqual(result.alerts.first?.text, "argocd/grafana")
        XCTAssertTrue(result.next.gitopsIssues.isEmpty)
        XCTAssertTrue(evaluate(previous: result.next, current: snap([:]), now: now).alerts.isEmpty)
    }

    func testAnUnreadableCheckKeepsWhatWasKnown() {
        var known = snap([:])
        known.gitopsIssues = [failed: failedValue]
        known.gitopsPending = [drift]
        let result = evaluate(previous: known, current: snap(nil), now: now)
        XCTAssertTrue(result.alerts.isEmpty)
        XCTAssertEqual(result.next.gitopsIssues, known.gitopsIssues)
        XCTAssertEqual(result.next.gitopsPending, known.gitopsPending)
        XCTAssertTrue(result.next.gitopsChecked)
    }

    func testTurningWatchingOffForgetsAndOnAgainIsABaseline() {
        var known = snap([:])
        known.gitopsIssues = [failed: failedValue]
        let off = evaluate(previous: known, current: snap(nil, watched: false), now: now)
        XCTAssertTrue(off.alerts.isEmpty)
        XCTAssertTrue(off.next.gitopsIssues.isEmpty)
        XCTAssertFalse(off.next.gitopsWatched)
        XCTAssertTrue(evaluate(previous: off.next, current: snap([failed: failedValue]), now: now).alerts.isEmpty)
    }

    func testAnotherClusterIsABaseline() {
        let result = evaluate(previous: snap([:], context: "lab"), current: snap([failed: failedValue], context: "prod"), now: now)
        XCTAssertTrue(result.alerts.isEmpty)
    }

    func testIndependentOfDataServices() {
        // A data issue and a GitOps issue: each track alerts on its own, with its own key.
        let result = evaluate(previous: snap([:]), current: snap([failed: failedValue], data: ["cnpg|db/down": dataCritical]), now: now)
        XCTAssertEqual(result.alerts.map(\.key), ["data:cnpg|db/down", "gitops:argocd|argocd/grafana"])
        // GitOps unreadable: the data track still alerts on its changes.
        let next = evaluate(previous: result.next, current: snap(nil, data: [:]), now: now)
        XCTAssertEqual(next.alerts.map(\.key), ["data:cnpg|db/down"])
        XCTAssertEqual(next.next.gitopsIssues, [failed: failedValue])
    }

    func testSnapshotOfKeepsUnreadableApart() throws {
        let overview = try TalosJSON.decode(ClusterOverview.self, from: #"{"context":"lab","nodes":[]}"#)
        let unread = snapshotOf(overview, etcd: nil, certNotAfter: 0, takenAt: now, gitopsWatched: true, gitopsIssues: nil)
        XCTAssertTrue(unread.gitopsWatched)
        XCTAssertFalse(unread.gitopsChecked)
        let read = snapshotOf(overview, etcd: nil, certNotAfter: 0, takenAt: now, gitopsWatched: true, gitopsIssues: [failed: failedValue])
        XCTAssertTrue(read.gitopsChecked)
        XCTAssertEqual(read.gitopsIssues, [failed: failedValue])
        let off = snapshotOf(overview, etcd: nil, certNotAfter: 0, takenAt: now, gitopsWatched: false, gitopsIssues: [failed: failedValue])
        XCTAssertFalse(off.gitopsChecked)
        XCTAssertTrue(off.gitopsIssues.isEmpty)
    }

    func testOlderSnapshotsStillDecode() throws {
        let old = #"{"context":"lab","takenAt":0,"nodes":{},"dataWatched":true,"dataChecked":true,"dataIssues":{"cnpg|db/down":"critical"}}"#
        let decoded = try JSONDecoder().decode(ClusterSnapshot.self, from: Data(old.utf8))
        XCTAssertFalse(decoded.gitopsWatched)
        XCTAssertFalse(decoded.gitopsChecked)
        XCTAssertTrue(decoded.gitopsIssues.isEmpty)
        XCTAssertTrue(decoded.gitopsPending.isEmpty)
        XCTAssertEqual(decoded.dataIssues, ["cnpg|db/down": dataCritical])

        let roundTrip = try JSONDecoder().decode(ClusterSnapshot.self, from: JSONEncoder().encode(snap([failed: failedValue])))
        XCTAssertEqual(roundTrip.gitopsIssues, [failed: failedValue])
        XCTAssertTrue(roundTrip.gitopsWatched)
    }

    func testKnownIssuesOnlyFromTheSameWatchedCluster() {
        let issues = [failed: failedValue]
        XCTAssertEqual(knownGitOpsIssues(snap(issues), context: "lab"), issues)
        // Another cluster, a check that could not read, or watching off: nothing carries over.
        XCTAssertEqual(knownGitOpsIssues(snap(issues, context: "other"), context: "lab"), [:])
        XCTAssertEqual(knownGitOpsIssues(snap(nil), context: "lab"), [:])
        XCTAssertEqual(knownGitOpsIssues(snap(issues, watched: false), context: "lab"), [:])
        XCTAssertEqual(knownGitOpsIssues(nil, context: "lab"), [:])
    }
}
