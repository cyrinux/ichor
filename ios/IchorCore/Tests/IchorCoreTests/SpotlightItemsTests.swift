import XCTest
@testable import IchorCore

final class SpotlightItemsTests: XCTestCase {
    private let contexts = [
        ContextSummary(name: "home-admin", fingerprint: "fa", clusterID: "homeaaaaaaaaaaaa"),
        ContextSummary(name: "home-reader", fingerprint: "fb", clusterID: "homeaaaaaaaaaaaa"),
        ContextSummary(name: "lab", fingerprint: "fc", clusterID: "labbbbbbbbbbbbbb"),
        ContextSummary(name: "old", fingerprint: "fd"),
    ]

    func testOneItemPerClusterWithAShareLinkNamedAsOnScreen() {
        let labels = ClusterLabels(names: ["fb": "Home"])
        let items = SpotlightItems.clusters(contexts, active: "home-reader", labels: labels)
        XCTAssertEqual(items.map(\.title), ["Home", "lab"])
        XCTAssertEqual(items.map(\.target), [
            ShareTarget(cluster: "homeaaaaaaaaaaaa", target: .cluster),
            ShareTarget(cluster: "labbbbbbbbbbbbbb", target: .cluster),
        ])
        XCTAssertEqual(Set(items.map(\.domain)), [SpotlightItems.clustersDomain])
        XCTAssertEqual(items.map(\.kind), [.cluster, .cluster])
    }

    func testNoClusterNoItem() {
        XCTAssertEqual(SpotlightItems.clusters([], active: nil, labels: ClusterLabels()), [])
    }

    func testMaskedLabelsHideGivenNames() {
        let items = SpotlightItems.clusters(contexts, active: "home-reader", labels: ClusterLabels(names: ["fb": "Home"], masked: true))
        XCTAssertEqual(items.first?.title, "home-reader")
    }

    func testGitOpsAppsLinkToTheirScreenOnTheirCluster() throws {
        let argo = try TalosJSON.decode(ArgoStatus.self, from: """
        {"installed":true,"apps":[{"namespace":"argocd","name":"grafana"},{"namespace":"argocd","name":"grafana"},{"namespace":"argocd","name":""}]}
        """)
        let flux = try TalosJSON.decode(FluxStatus.self, from: """
        {"installed":true,"apps":[{"kind":"HelmRelease","namespace":"flux-system","name":"podinfo"}]}
        """)
        let lab = contexts[2]
        let argoItems = SpotlightItems.argoApps(argo.apps, of: lab, labels: ClusterLabels())
        XCTAssertEqual(argoItems.count, 1)
        XCTAssertEqual(argoItems.first?.title, "grafana")
        XCTAssertEqual(argoItems.first?.clusterLabel, "lab")
        XCTAssertEqual(argoItems.first?.kind, .argoApp)
        XCTAssertEqual(argoItems.first?.domain, "apps.labbbbbbbbbbbbbb.argo")
        XCTAssertEqual(argoItems.first?.target, ShareTarget(cluster: "labbbbbbbbbbbbbb", target: .argoApp, namespace: "argocd", name: "grafana"))

        let fluxItems = SpotlightItems.fluxApps(flux.apps, of: lab, labels: ClusterLabels())
        XCTAssertEqual(fluxItems.first?.domain, "apps.labbbbbbbbbbbbbb.flux")
        XCTAssertEqual(fluxItems.first?.target,
                       ShareTarget(cluster: "labbbbbbbbbbbbbb", target: .fluxApp, kind: "HelmRelease", namespace: "flux-system", name: "podinfo"))

        // No cluster id: no link could open them.
        XCTAssertEqual(SpotlightItems.argoApps(argo.apps, of: contexts[3], labels: ClusterLabels()), [])
    }

    func testAppDomainsNestUnderApps() {
        XCTAssertTrue(SpotlightItems.appsDomain(cluster: "abc").hasPrefix(SpotlightItems.appsDomain + "."))
        XCTAssertTrue(SpotlightItems.appsDomain(cluster: "abc", kind: .fluxApp).hasPrefix(SpotlightItems.appsDomain(cluster: "abc") + "."))
    }

    func testStaleAppClustersAreTheRemovedOnes() {
        XCTAssertEqual(SpotlightItems.staleAppClusters(indexed: ["homeaaaaaaaaaaaa", "gonexxxxxxxxxxxx"], current: contexts),
                       ["gonexxxxxxxxxxxx"])
    }

    func testIdentifierOpensTheInAppLink() {
        XCTAssertEqual(SpotlightItems.link(identifier: "https://example.org/ichor/open/#v=1&c=abc&t=cluster"),
                       "ichor://open?v=1&c=abc&t=cluster")
        XCTAssertNil(SpotlightItems.link(identifier: "ichor://open?v=1"))
        XCTAssertNil(SpotlightItems.link(identifier: "fingerprint-1"))
    }
}
