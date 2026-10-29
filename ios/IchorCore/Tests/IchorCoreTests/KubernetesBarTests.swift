import XCTest
@testable import IchorCore

final class KubernetesBarTests: XCTestCase {
    func testDefaultKeepsThreeIconsSoTheTitleFits() {
        let bar = KubernetesBar()
        XCTAssertEqual(bar.icons, [.checkup, .networkPolicies, .share])
        XCTAssertEqual(
            bar.menu,
            [
                .apiHealth, .flows, .resources, .helm, .storage, .services, .jobs, .events, .admissionPolicies,
                .autoscaling, .virtualMachines, .crossplane, .tailscale, .argoRollouts, .clusterLifecycle,
                .dataServices, .certManager, .castai, .capacity,
            ])
        XCTAssertTrue(bar.isDefault)
        XCTAssertEqual(KubernetesBar.storageKey, "kubernetes.bar")
    }

    /// The same names in the same order as Android's KubernetesAction (KubernetesBarTest.theSameActionsAsIos).
    func testTheSameActionsAsAndroid() {
        XCTAssertEqual(
            KubernetesAction.allCases.map(\.rawValue).joined(separator: ","),
            "CHECKUP,NETWORK_POLICIES,SHARE,API_HEALTH,FLOWS,RESOURCES,HELM,STORAGE,SERVICES,JOBS,EVENTS,POLICIES,AUTOSCALING,VIRTUAL_MACHINES,CROSSPLANE,TAILSCALE,ARGO_ROLLOUTS,CLUSTER_LIFECYCLE,DATA_SERVICES,CERT_MANAGER,CASTAI,CAPACITY"
        )
        XCTAssertEqual(KubernetesAction.allCases.count, 22)
    }

    func testEncodeMatchesAndroid() {
        let bar = KubernetesBar().toBar(.flows).toMenu(.checkup)
        XCTAssertEqual(
            bar.encoded,
            "NETWORK_POLICIES,SHARE,FLOWS|CHECKUP,API_HEALTH,RESOURCES,HELM,STORAGE,SERVICES,JOBS,EVENTS,POLICIES,AUTOSCALING,VIRTUAL_MACHINES,CROSSPLANE,TAILSCALE,ARGO_ROLLOUTS,CLUSTER_LIFECYCLE,DATA_SERVICES,CERT_MANAGER,CASTAI,CAPACITY"
        )
        XCTAssertEqual(KubernetesBar.parse(bar.encoded), bar)
        XCTAssertFalse(bar.isDefault)
    }

    func testABarSavedWithCleanUpDropsItAndGetsTheNewActionsLast() {
        let bar = KubernetesBar.parse("CHECKUP,FLOWS|SHARE,CLEAN_UP_PODS,RESOURCES")
        XCTAssertEqual(bar.icons, [.checkup, .flows])
        XCTAssertFalse(bar.encoded.contains("CLEAN_UP_PODS"))
        XCTAssertEqual(Array(bar.menu.prefix(2)), [.share, .resources])
        XCTAssertEqual(bar.menu.last, .capacity)
        XCTAssertEqual(Set(bar.icons + bar.menu), Set(KubernetesAction.allCases))
    }

    /// The API address is set from the cluster's row now: a bar saved with it drops it, nothing else moves.
    func testABarSavedWithTheApiAddressDropsIt() {
        let bar = KubernetesBar.parse("CHECKUP,API_ADDRESS|SHARE,API_ADDRESS,RESOURCES")
        XCTAssertEqual(bar.icons, [.checkup])
        XCTAssertFalse(bar.encoded.contains("API_ADDRESS"))
        XCTAssertEqual(Array(bar.menu.prefix(2)), [.share, .resources])
        XCTAssertEqual(Set(bar.icons + bar.menu), Set(KubernetesAction.allCases))
    }

    func testTheMenuReadsInSectionsAndASavedOrderHoldsInside() {
        let sections = groupedMenu(KubernetesBar().menu)
        XCTAssertEqual(sections.map(\.group), [.explore, .health, .installed])
        XCTAssertEqual(sections[0].actions, [.resources, .helm, .storage, .services, .jobs, .events])
        XCTAssertEqual(sections[1].actions, [.apiHealth, .flows, .capacity])
        // Events moved before Resources by the user: still in Explore, still first there.
        let saved = KubernetesBar.parse("CHECKUP|EVENTS,CROSSPLANE,RESOURCES")
        let explore = groupedMenu(saved.menu).first { $0.group == .explore }?.actions ?? []
        XCTAssertEqual(Array(explore.prefix(2)), [.events, .resources])
        // A section with nothing offered is left out.
        XCTAssertEqual(groupedMenu([.helm]).map(\.group), [.explore])
    }

    func testABarSavedBeforeTheOperatorScreensGetsThemLastInItsMenu() {
        let bar = KubernetesBar.parse("CHECKUP,FLOWS|SHARE,API_HEALTH")
        XCTAssertEqual(bar.icons, [.checkup, .flows])
        XCTAssertEqual(
            Array(bar.menu.dropLast(4).suffix(4)), [.crossplane, .tailscale, .argoRollouts, .clusterLifecycle])
    }

    func testABarSavedBeforeTheDataServicesEntriesGetsThemLastInItsMenu() {
        let bar = KubernetesBar.parse("CHECKUP,CROSSPLANE|SHARE,CLUSTER_LIFECYCLE")
        XCTAssertEqual(bar.icons, [.checkup, .crossplane])
        XCTAssertEqual(Array(bar.menu.suffix(4)), [.dataServices, .certManager, .castai, .capacity])
        // Sealed Secrets and ExternalDNS are plugins now: a bar saved with them drops their entries.
        XCTAssertFalse(KubernetesBar.parse("CHECKUP,SEALED_SECRETS|SHARE").encoded.contains("SEALED_SECRETS"))
        let saved = KubernetesBar.parse("CHECKUP,EXTERNAL_DNS|SHARE,CERT_MANAGER")
        XCTAssertEqual(saved.icons, [.checkup])
        XCTAssertFalse(saved.encoded.contains("EXTERNAL_DNS"))
        XCTAssertEqual(KubernetesAction.certManager.dataServiceKind, .certManager)
        XCTAssertNil(KubernetesAction.dataServices.dataServiceKind)
    }

    func testAnotherScreensNamesAreNotTaken() {
        let bar = KubernetesBar.parse("HEALTH,ETCD|METRICS")
        XCTAssertTrue(bar.icons.isEmpty)
        XCTAssertEqual(bar.menu, KubernetesAction.allCases)
    }
}
