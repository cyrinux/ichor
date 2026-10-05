import Foundation
import XCTest
@testable import IchorCore

final class KubeScopeTests: XCTestCase {
    func testStoredScopes() {
        XCTAssertEqual(KubeScope.fromStored(nil), KubeScope())
        XCTAssertEqual(KubeScope.fromStored(""), KubeScope(chosen: true))
        XCTAssertEqual(KubeScope.fromStored("shop"), KubeScope(namespace: "shop", chosen: true))
        XCTAssertNil(KubeScope().stored)
        XCTAssertEqual(KubeScope(chosen: true).stored, "")
        XCTAssertEqual(KubeScope(namespace: "shop", chosen: true).stored, "shop")
    }

    func testDefaultIsEveryNamespaceUnlessRemembered() {
        let listed = KubeNamespaces(namespaces: ["a", "shop"])
        XCTAssertEqual(defaultScope(remembered: nil, namespaces: nil), KubeScope())
        XCTAssertEqual(defaultScope(remembered: nil, namespaces: listed), KubeScope())
        let shop = KubeScope(namespace: "shop", chosen: true)
        XCTAssertEqual(defaultScope(remembered: shop, namespaces: listed), shop)
    }

    func testForbiddenNamespacesFallBackToTheContextOne() {
        let forbidden = KubeNamespaces(forbidden: true, contextNamespace: "team-a")
        XCTAssertEqual(defaultScope(remembered: nil, namespaces: forbidden), KubeScope(namespace: "team-a"))
        // A typed one wins.
        let typed = KubeScope(namespace: "team-b", chosen: true)
        XCTAssertEqual(defaultScope(remembered: typed, namespaces: forbidden), typed)
        // No namespace to fall back to: every namespace would be refused too, the user types one.
        XCTAssertNil(defaultScope(remembered: nil, namespaces: KubeNamespaces(forbidden: true)))
    }

    func testRecognisesARefusedList() {
        XCTAssertTrue(isKubeForbidden("Kubernetes API: permission denied: pods is forbidden"))
        XCTAssertFalse(isKubeForbidden("Kubernetes API: not found: x"))
    }

    func testOnlyAChosenScopeLoadsEagerlyUpToTheCap() {
        XCTAssertEqual(eagerLimit(scope: KubeScope(), metered: false), kubePageSize)
        XCTAssertEqual(eagerLimit(scope: KubeScope(chosen: true), metered: false), 10_000)
        XCTAssertEqual(eagerLimit(scope: KubeScope(namespace: "shop", chosen: true), metered: true), 5_000)
        // The context namespace, not chosen but narrow, loads in full too.
        XCTAssertEqual(eagerLimit(scope: KubeScope(namespace: "team-a"), metered: false), 10_000)
    }

    func testChoicesFromTheClusterElseTheLoadedRows() {
        let typed = KubeScope(namespace: "typed", chosen: true)
        XCTAssertEqual(scopeChoices(listed: KubeNamespaces(namespaces: ["b", "a"]), loaded: ["x"], scope: typed), ["a", "b", "typed"])
        XCTAssertEqual(scopeChoices(listed: nil, loaded: ["y", "x"], scope: KubeScope()), ["x", "y"])
        XCTAssertEqual(scopeChoices(listed: KubeNamespaces(forbidden: true), loaded: ["x"], scope: KubeScope()), ["x"])
        XCTAssertEqual(matchingNamespaces(["argocd", "kube-system"], query: "SYSTEM"), ["kube-system"])
        XCTAssertEqual(matchingNamespaces(["argocd", "kube-system"], query: " "), ["argocd", "kube-system"])
    }

    func testRemembersTheScopePickedPerCluster() {
        var scopes: [String: String] = [:]
        scopes = settingKubeScope(KubeScope(namespace: "shop", chosen: true), for: "fp1", in: scopes)
        scopes = settingKubeScope(KubeScope(chosen: true), for: "fp2", in: scopes)
        scopes = settingKubeScope(KubeScope(namespace: "ignored", chosen: true), for: "", in: scopes)
        XCTAssertEqual(scopes, ["fp1": "shop", "fp2": ""])
        XCTAssertEqual(KubeScope.fromStored(scopes["fp1"]), KubeScope(namespace: "shop", chosen: true))
        XCTAssertEqual(KubeScope.fromStored(scopes["fp3"]), KubeScope())
        // A scope not chosen (the default) forgets the cluster's.
        scopes = settingKubeScope(KubeScope(), for: "fp1", in: scopes)
        XCTAssertEqual(scopes, ["fp2": ""])
    }

    func testDecodesTheGoNamespaces() throws {
        let ns = try TalosJSON.decode(KubeNamespaces.self, from: #"{"namespaces":["a"],"forbidden":false,"contextNamespace":"default"}"#)
        XCTAssertEqual(ns.namespaces, ["a"])
        XCTAssertEqual(ns.contextNamespace, "default")
        XCTAssertFalse(ns.forbidden)
        let refused = try TalosJSON.decode(KubeNamespaces.self, from: #"{"namespaces":null,"forbidden":true}"#)
        XCTAssertEqual(refused, KubeNamespaces(forbidden: true))
    }

    func testDecodesPages() throws {
        let pods = try TalosJSON.decode(KubePodPage.self, from: #"{"pods":[{"namespace":"a","name":"x"}],"continue":"6869","remaining":1500,"complete":false}"#)
            .page(detailed: false)
        XCTAssertEqual(pods.continueToken, "6869")
        XCTAssertEqual(pods.remaining, 1500)
        XCTAssertFalse(pods.complete)
        XCTAssertFalse(pods.detailed)
        XCTAssertEqual(pods.items.map(\.name), ["x"])

        let workloads = try TalosJSON.decode(KubeWorkloadPage.self, from: #"{"workloads":[{"kind":"Deployment","namespace":"a","name":"web"}],"continue":"","remaining":0,"complete":true}"#).page
        XCTAssertTrue(workloads.complete)
        XCTAssertEqual(workloads.items.map(\.id), ["Deployment/a/web"])

        let cronJobs = try TalosJSON.decode(KubeCronJobPage.self, from: #"{"cronJobs":[{"namespace":"a","name":"backup"}],"continue":"ab","remaining":-1,"complete":false}"#).page
        XCTAssertEqual(cronJobs.remaining, -1)
        XCTAssertEqual(cronJobs.continueToken, "ab")
    }

    func testKeptListsRoundTrip() throws {
        // Complete lists are kept as JSON arrays of the rows (last known state).
        let pods = [KubePod(namespace: "a", name: "x", status: "Running", images: ["nginx"], containerNames: ["web"])]
        let json = String(decoding: try JSONEncoder().encode(pods), as: UTF8.self)
        XCTAssertEqual(try TalosJSON.decode([KubePod].self, from: json), pods)
        let crons = [KubeCronJob(namespace: "a", name: "b", runs: [KubeJobRun(name: "b-1", state: "succeeded")])]
        let cronJSON = String(decoding: try JSONEncoder().encode(crons), as: UTF8.self)
        XCTAssertEqual(try TalosJSON.decode([KubeCronJob].self, from: cronJSON), crons)
    }

    func testWorkloadKindsAreMergedIntoOnePage() throws {
        // Deployments: two pages; StatefulSets: one; DaemonSets: one.
        final class Asked: @unchecked Sendable {
            let lock = NSLock()
            var calls: [String] = []
            func add(_ call: String) { lock.lock(); calls.append(call); lock.unlock() }
        }
        let asked = Asked()
        let fetch: @Sendable (String, String) async throws -> KubePage<KubeWorkload> = { kind, token in
            asked.add("\(kind)=\(token)")
            if kind == "Deployment" && token.isEmpty {
                return KubePage(items: [KubeWorkload(kind: kind, namespace: "a", name: "web")], continueToken: "d1", remaining: 1, complete: false)
            }
            if kind == "Deployment" { return KubePage(items: [KubeWorkload(kind: kind, namespace: "a", name: "api")]) }
            return KubePage(items: [KubeWorkload(kind: kind, namespace: "a", name: kind.lowercased())])
        }

        let first = try runBlocking { try await fetchWorkloadPage("", fetch: fetch) }
        XCTAssertEqual(first.items.map(\.name), ["web", "statefulset", "daemonset"])
        XCTAssertFalse(first.complete)
        XCTAssertEqual(first.continueToken, "Deployment=d1")
        XCTAssertEqual(first.remaining, 1)

        let second = try runBlocking { try await fetchWorkloadPage(first.continueToken, fetch: fetch) }
        XCTAssertTrue(second.complete)
        XCTAssertEqual(second.items.map(\.name), ["api"])
        XCTAssertEqual(Set(asked.calls), ["Deployment=", "StatefulSet=", "DaemonSet=", "Deployment=d1"])
        XCTAssertEqual(asked.calls.count, 4)
    }

    func testWorkloadTokens() {
        XCTAssertEqual(parseWorkloadToken(""), ["Deployment": "", "StatefulSet": "", "DaemonSet": ""])
        XCTAssertEqual(parseWorkloadToken("StatefulSet=ab;DaemonSet=cd;Bogus=1"), ["StatefulSet": "ab", "DaemonSet": "cd"])
        XCTAssertEqual(workloadToken(["DaemonSet": "b", "Deployment": "a"]), "Deployment=a;DaemonSet=b")
    }

    func testAnUncountedKindLeavesTheTotalUnknown() throws {
        let page = try runBlocking {
            try await fetchWorkloadPage("") { kind, _ in
                KubePage(items: [], continueToken: "x", remaining: kind == "DaemonSet" ? -1 : 3, complete: false)
            }
        }
        XCTAssertEqual(page.remaining, -1)
    }

    func testIncompleteListsKeepTheirOrderAndTableRowsSkipImages() {
        let pods = [
            KubePod(namespace: "shop", name: "web-1", status: "Running", healthy: true, images: ["nginx:1.27"]),
            KubePod(namespace: "a", name: "worker-1", status: "CrashLoopBackOff"),
        ]
        // Still loading: server order, so rows do not jump as pages arrive.
        XCTAssertEqual(filterPods(pods, namespace: nil, query: "", sorted: false).map(\.name), ["web-1", "worker-1"])
        XCTAssertEqual(filterPods(pods, namespace: nil, query: "").map(\.name), ["worker-1", "web-1"])
        // Some rows came from a Table (no images): an image match would be partial.
        XCTAssertEqual(filterPods(pods, namespace: nil, query: "nginx", searchImages: false).map(\.name), [])
        XCTAssertEqual(filterPods(pods, namespace: nil, query: "nginx").map(\.name), ["web-1"])

        let workloads = [KubeWorkload(kind: "Deployment", namespace: "b", name: "web", state: "ready"),
                         KubeWorkload(kind: "Deployment", namespace: "a", name: "api", state: "degraded")]
        XCTAssertEqual(filterWorkloads(workloads, namespace: nil, query: "", sorted: false).map(\.name), ["web", "api"])
        let crons = [KubeCronJob(namespace: "b", name: "z"), KubeCronJob(namespace: "a", name: "y")]
        XCTAssertEqual(filterCronJobs(crons, namespace: nil, query: "", sorted: false).map(\.name), ["z", "y"])
        XCTAssertEqual(filterCronJobs(crons, namespace: nil, query: "").map(\.name), ["y", "z"])
    }
}
