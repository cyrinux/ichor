import XCTest
@testable import IchorCore

final class ShareTargetTests: XCTestCase {
    private let contexts = [
        ContextSummary(name: "admin@prod", clusterID: "prodprodprodprod"),
        ContextSummary(name: "reader@lab", clusterID: "lablablablablabl"),
        ContextSummary(name: "admin@lab", clusterID: "lablablablablabl"),
        ContextSummary(name: "admin@legacy"),
    ]

    func testLinkOpensTheActiveContextOfItsCluster() {
        XCTAssertEqual(contexts.context(forCluster: "lablablablablabl", active: "admin@lab")?.name, "admin@lab")
    }

    func testLinkOpensTheFirstContextOfItsClusterOtherwise() {
        XCTAssertEqual(contexts.context(forCluster: "lablablablablabl", active: "admin@prod")?.name, "reader@lab")
    }

    func testLinkOfAnUnknownClusterOpensNothing() {
        XCTAssertNil(contexts.context(forCluster: "otherotherotherx", active: "admin@prod"))
        // A summary from a core without cluster ids never matches an empty one.
        XCTAssertNil(contexts.context(forCluster: "", active: "admin@legacy"))
    }

    func testClusterIDDecodesAndDefaultsToEmpty() throws {
        let json = #"{"current":"a","contexts":[{"name":"a","clusterId":"abcdefghijklmnop"},{"name":"b"}]}"#
        let summary = try TalosJSON.decode(ConfigSummary.self, from: json)
        XCTAssertEqual(summary.contexts.map(\.clusterID), ["abcdefghijklmnop", ""])
    }

    func testGoJSONDecodes() throws {
        let json = #"{"cluster":"lablablablablabl","target":"flux-app","kind":"HelmRelease","ns":"flux-system","name":"podinfo"}"#
        let target = try TalosJSON.decode(ShareTarget.self, from: json)
        var want = ShareTarget.fluxApp(kind: "HelmRelease", namespace: "flux-system", name: "podinfo")
        want.cluster = "lablablablablabl"
        XCTAssertEqual(target, want)
    }

    func testEncodedForGoWithoutEmptyFields() throws {
        let encoder = JSONEncoder()
        encoder.outputFormatting = .sortedKeys
        let json = String(decoding: try encoder.encode(ShareTarget.argoApp(namespace: "argocd", name: "guestbook")), as: UTF8.self)
        XCTAssertEqual(json, #"{"name":"guestbook","ns":"argocd","target":"argo-app"}"#)
    }

    func testNodeTabsOutsideTheScreenAreDropped() {
        XCTAssertEqual(ShareTarget.node(address: "10.0.0.2", hostname: "cp-1", tab: "live").tab, "live")
        XCTAssertEqual(ShareTarget.node(address: "10.0.0.2", hostname: "cp-1", tab: "Live").tab, "")
    }

    func testKubernetesTargetsFocusTheirTabAndRow() {
        XCTAssertEqual(ShareTarget.kubernetes(tab: .cronJobs).kubeFocus, KubeFocus(tab: .cronJobs))
        XCTAssertEqual(ShareTarget(target: .workloads, tab: "network").kubeFocus, KubeFocus(tab: .workloads))
        XCTAssertEqual(ShareTarget.workload(kind: "StatefulSet", namespace: "db", name: "postgres").kubeFocus,
                       KubeFocus(tab: .workloads, id: "StatefulSet/db/postgres", namespace: "db", name: "postgres"))
        XCTAssertEqual(ShareTarget.pod(namespace: "db", name: "postgres-0").kubeFocus,
                       KubeFocus(tab: .pods, id: "db/postgres-0", namespace: "db", name: "postgres-0"))
        XCTAssertEqual(ShareTarget.cronJob(namespace: "ops", name: "backup").kubeFocus,
                       KubeFocus(tab: .cronJobs, id: "ops/backup", namespace: "ops", name: "backup"))
        XCTAssertNil(ShareTarget.argoApp(namespace: "argocd", name: "guestbook").kubeFocus)
    }

    func testDataTargetNamesItsTabByCatalogID() throws {
        let json = String(decoding: try JSONEncoder().encode(ShareTarget.data(.cnpg)), as: UTF8.self)
        XCTAssertTrue(json.contains(#""kind":"cloudnative-pg""#))
        XCTAssertEqual(try TalosJSON.decode(ShareTarget.self, from: json).dataServiceKind, .cnpg)
        // A system this app does not know, or none: the first tab.
        XCTAssertNil(ShareTarget(target: .data, kind: "newdb").dataServiceKind)
        XCTAssertEqual(ShareTarget.data(nil), ShareTarget(target: .data))
        XCTAssertEqual(try TalosJSON.decode(ShareTarget.self, from: #"{"target":"checkup"}"#), .screen(.checkup))
    }

    func testDataAlertSystemsMapToCatalogIDs() {
        let systems = ["longhorn", "garage", "cnpg", "dragonfly", "mariadb", "percona", "certmanager", "velero", "ceph", "castai"]
        XCTAssertEqual(systems.map { DataServiceKind(alertKey: "\($0)|x")?.catalogID },
                       ["longhorn", "garage", "cloudnative-pg", "dragonfly", "mariadb", "percona-xtradb", "cert-manager",
                        "velero", "rook", "castai"])
        XCTAssertNil(DataServiceKind(alertKey: "other|x"))
    }

    func testAlertsOpenWhatTheyAreAbout() {
        let talos = ClusterSnapshot(context: "lab", takenAt: Date(), nodes: ["10.0.0.2": NodeState(hostname: "cp-1", health: .notReady)])
        var kube = talos
        kube.kube = true
        func target(_ key: String, _ snapshot: ClusterSnapshot = talos) -> ShareTarget? { .forAlert(key: key, snapshot: snapshot) }
        XCTAssertEqual(target("node:10.0.0.2"), .node(address: "10.0.0.2", hostname: "cp-1", tab: ""))
        XCTAssertEqual(target("etcd:cp-1:NOSPACE"), .screen(.etcd))
        XCTAssertNil(target("cert"))
        XCTAssertEqual(target("cert", kube), .screen(.cluster))
        XCTAssertEqual(target("data:velero|BackupStorageLocation/default"), .data(.velero))
        XCTAssertEqual(target("data:other|x"), .data(nil))
        XCTAssertEqual(target("gitops:argocd|argocd/guestbook"), .argoApp(namespace: "argocd", name: "guestbook"))
        XCTAssertEqual(target("gitops:flux|HelmRelease flux-system/podinfo"),
                       .fluxApp(kind: "HelmRelease", namespace: "flux-system", name: "podinfo"))
        XCTAssertEqual(target("checkup:pods|CrashLoopBackOff|db/postgres-0"), .screen(.checkup))
        XCTAssertNil(target("other"))
    }
}
