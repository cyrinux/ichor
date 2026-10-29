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
        ClusterSnapshot(
            context: context, takenAt: now, nodes: ["a": NodeState(hostname: "host-a", health: .ready)],
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
        XCTAssertEqual(
            try report().alertIssues,
            [
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
        XCTAssertEqual(
            CheckupSubject(key: "storage|volumeFull|shop/data"), CheckupSubject(key: "storage|volumeFull|shop/data"))
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
        let list = try TalosJSON.decode(
            KubeEventList.self,
            from:
                #"{"events":[{"type":"Warning","reason":"BackOff","message":"m","kind":"Pod","name":"p","count":4,"last":5},{"type":"Normal","reason":"Pulled"}]}"#
        )
        XCTAssertEqual(list.events.map(\.isWarning), [true, false])
        XCTAssertEqual(list.events[1].count, 1)
    }

    // The demo checkup's Crossplane section (go/ichorgo/kube_checkup_demo.go).
    func testCrossplaneSectionDecodesAndAlerts() throws {
        let r = try TalosJSON.decode(
            CheckupReport.self,
            from: #"""
                {"status":"critical","sections":[{"id":"crossplane","status":"critical","checked":12,"findings":[
                  {"kind":"crossplaneSyncFailed","severity":"critical","namespace":"apps","name":"Database/app-db-primary","reason":"ReconcileError","message":"quota exceeded","extra":"default"},
                  {"kind":"crossplaneProviderUnhealthy","severity":"critical","name":"Provider/provider-storage","reason":"UnhealthyPackageRevision","message":"cannot pull package","extra":"xpkg.example.org/demo/provider-storage:v1.4.0"},
                  {"kind":"crossplaneNotReady","severity":"warning","name":"Bucket/assets-bucket","reason":"Creating","extra":"default","count":42,"since":1}]}]}
                """#)
        let section = try XCTUnwrap(r.shownSections.first)
        XCTAssertEqual(section.section, .crossplane)
        XCTAssertEqual(
            section.findings.compactMap(\.kind),
            [.crossplaneSyncFailed, .crossplaneProviderUnhealthy, .crossplaneNotReady])
        XCTAssertEqual(section.findings[0].detail, "ReconcileError: quota exceeded")
        XCTAssertEqual(
            r.alertIssues,
            [
                "crossplane|crossplaneSyncFailed|apps/Database/app-db-primary": "critical",
                "crossplane|crossplaneProviderUnhealthy|Provider/provider-storage": "critical",
                "crossplane|crossplaneNotReady|Bucket/assets-bucket": "warning",
            ])
    }

    // The demo checkup's Tailscale section (go/ichorgo/kube_checkup_demo.go), plus a proxy group.
    func testTailscaleSectionDecodesAndAlerts() throws {
        let r = try TalosJSON.decode(
            CheckupReport.self,
            from: #"""
                {"status":"critical","sections":[{"id":"tailscale","status":"critical","checked":6,"findings":[
                  {"kind":"tailscaleConnectorNotReady","severity":"critical","name":"lab-router","reason":"ConnectorCreationFailed","message":"failed to create the subnet router","extra":"subnet-router"},
                  {"kind":"tailscaleProxyGroupDown","severity":"warning","name":"egress-proxies","reason":"ProxyGroupNotReady","extra":"egress"}]}]}
                """#)
        let section = try XCTUnwrap(r.shownSections.first)
        XCTAssertEqual(section.section, .tailscale)
        XCTAssertEqual(section.findings.compactMap(\.kind), [.tailscaleConnectorNotReady, .tailscaleProxyGroupDown])
        XCTAssertEqual(section.findings[0].detail, "ConnectorCreationFailed: failed to create the subnet router")
        XCTAssertEqual(
            r.alertIssues,
            [
                "tailscale|tailscaleConnectorNotReady|lab-router": "critical",
                "tailscale|tailscaleProxyGroupDown|egress-proxies": "warning",
            ])
    }

    // The demo checkup's Cluster lifecycle section (go/ichorgo/kube_checkup_demo.go), plus the unaccepted summary.
    func testClusterAPISectionDecodesAndAlerts() throws {
        let r = try TalosJSON.decode(
            CheckupReport.self,
            from: #"""
                {"status":"critical","sections":[{"id":"clusterapi","status":"critical","checked":19,"findings":[
                  {"kind":"sideroServerOff","severity":"critical","name":"srv-b2","extra":"rack2-u02"},
                  {"kind":"capiMachineStuck","severity":"warning","namespace":"fleet","name":"staging-cp-x7k2p","extra":"staging/Provisioning","since":1},
                  {"kind":"sideroServerUnaccepted","severity":"warning","name":"","count":3}]}]}
                """#)
        let section = try XCTUnwrap(r.shownSections.first)
        XCTAssertEqual(section.section, .clusterapi)
        XCTAssertEqual(
            section.findings.compactMap(\.kind), [.sideroServerOff, .capiMachineStuck, .sideroServerUnaccepted])
        XCTAssertEqual(
            r.alertIssues,
            [
                "clusterapi|sideroServerOff|srv-b2": "critical",
                "clusterapi|capiMachineStuck|fleet/staging-cp-x7k2p": "warning",
                "clusterapi|sideroServerUnaccepted|": "warning",
            ])
    }

    // The demo checkup's SOPS finding (go/ichorgo/kube_checkup_demo.go): worded with its
    // reason, and the monitor alerts on it.
    func testSopsDecryptionFailedDecodesAndAlerts() throws {
        let r = try TalosJSON.decode(
            CheckupReport.self,
            from: #"""
                {"status":"critical","sections":[{"id":"secrets","status":"critical","checked":12,"findings":[
                  {"kind":"sopsDecryptionFailed","severity":"critical","namespace":"flux-system","name":"apps","extra":"sops-age","reason":"BuildFailed","message":"error decrypting sources: failed to decrypt: no key could decrypt the data"}]}]}
                """#)
        let f = try XCTUnwrap(r.shownSections.first?.findings.first)
        XCTAssertEqual(f.kind, .sopsDecryptionFailed)
        XCTAssertEqual(f.extra, "sops-age")
        XCTAssertEqual(
            f.detail, "BuildFailed: error decrypting sources: failed to decrypt: no key could decrypt the data")
        let issue = ["secrets|sopsDecryptionFailed|flux-system/apps": "critical"]
        XCTAssertEqual(r.alertIssues, issue)
        XCTAssertEqual(checkupIssuesWithGaps(r, known: [:], gitopsWatched: false), issue)
        // With GitOps watched, the Kustomization's Ready=False already alerts there.
        XCTAssertEqual(r.alertIssues(gitopsWatched: true), [:])
        XCTAssertEqual(checkupIssuesWithGaps(r, known: [:], gitopsWatched: true), [:])
        XCTAssertEqual(gitopsAlertedKinds, ["sopsDecryptionFailed"])
    }

    // The demo checkup's route certificate findings (go/ichorgo/kube_checkup_demo.go).
    func testRouteCertKindsDecode() throws {
        let r = try TalosJSON.decode(
            CheckupReport.self,
            from: #"""
                {"status":"warning","sections":[{"id":"certificates","status":"warning","checked":18,"findings":[
                  {"kind":"routeCertExpiring","severity":"warning","namespace":"default","name":"vaultwarden-tls","extra":"Ingress default/vaultwarden","reason":"vault.home.example","count":9,"since":1},
                  {"kind":"routeCertHostMismatch","severity":"warning","namespace":"default","name":"paperless-tls","extra":"Ingress default/paperless","reason":"paperless.home.example","message":"paperless.lan, *.lan"}]}]}
                """#)
        let section = try XCTUnwrap(r.shownSections.first)
        XCTAssertEqual(section.findings.compactMap(\.kind), [.routeCertExpiring, .routeCertHostMismatch])
        XCTAssertEqual(section.findings[1].detail, "paperless.lan, *.lan")
        XCTAssertEqual(r.alertIssues["certificates|routeCertExpiring|default/vaultwarden-tls"], "warning")
    }
}
