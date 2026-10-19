import XCTest
@testable import IchorCore

final class KubernetesBarTests: XCTestCase {
    func testDefaultKeepsThreeIconsSoTheTitleFits() {
        let bar = KubernetesBar()
        XCTAssertEqual(bar.icons, [.checkup, .networkPolicies, .share])
        XCTAssertEqual(bar.menu, [.apiHealth, .flows, .resources, .helm, .events, .apiAddress, .capacity, .admissionPolicies, .autoscaling, .virtualMachines, .cleanUpPods, .crossplane, .tailscale, .argoRollouts, .clusterLifecycle, .dataServices, .externalDns, .certManager, .sealedSecrets, .castai])
        XCTAssertTrue(bar.isDefault)
        XCTAssertEqual(KubernetesBar.storageKey, "kubernetes.bar")
    }

    func testEncodeMatchesAndroid() {
        let bar = KubernetesBar().toBar(.flows).toMenu(.checkup)
        XCTAssertEqual(bar.encoded, "NETWORK_POLICIES,SHARE,FLOWS|CHECKUP,API_HEALTH,RESOURCES,HELM,EVENTS,API_ADDRESS,CAPACITY,POLICIES,AUTOSCALING,VIRTUAL_MACHINES,CLEAN_UP_PODS,CROSSPLANE,TAILSCALE,ARGO_ROLLOUTS,CLUSTER_LIFECYCLE,DATA_SERVICES,EXTERNAL_DNS,CERT_MANAGER,SEALED_SECRETS,CASTAI")
        XCTAssertEqual(KubernetesBar.parse(bar.encoded), bar)
        XCTAssertFalse(bar.isDefault)
    }

    func testABarSavedBeforeTheOperatorScreensGetsThemLastInItsMenu() {
        let bar = KubernetesBar.parse("CHECKUP,FLOWS|SHARE,API_HEALTH")
        XCTAssertEqual(bar.icons, [.checkup, .flows])
        XCTAssertEqual(Array(bar.menu.dropLast(5).suffix(4)), [.crossplane, .tailscale, .argoRollouts, .clusterLifecycle])
    }

    func testABarSavedBeforeTheDataServicesEntriesGetsThemLastInItsMenu() {
        let bar = KubernetesBar.parse("CHECKUP,CROSSPLANE|SHARE,CLUSTER_LIFECYCLE")
        XCTAssertEqual(bar.icons, [.checkup, .crossplane])
        XCTAssertEqual(Array(bar.menu.suffix(5)), [.dataServices, .externalDns, .certManager, .sealedSecrets, .castai])
        XCTAssertEqual(KubernetesAction.certManager.dataServiceKind, .certManager)
        XCTAssertNil(KubernetesAction.dataServices.dataServiceKind)
    }

    func testAnotherScreensNamesAreNotTaken() {
        let bar = KubernetesBar.parse("HEALTH,ETCD|METRICS")
        XCTAssertTrue(bar.icons.isEmpty)
        XCTAssertEqual(bar.menu, KubernetesAction.allCases)
    }
}
