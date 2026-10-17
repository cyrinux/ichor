import XCTest
@testable import IchorCore

final class ObjectImageScanTests: XCTestCase {
    func testScanModeByKind() {
        XCTAssertEqual(KubeAPIResource(resource: "pods", kind: "Pod").objectScanMode, .pod)
        for (resource, kind) in [("deployments", "Deployment"), ("statefulsets", "StatefulSet"), ("daemonsets", "DaemonSet")] {
            XCTAssertEqual(KubeAPIResource(group: "apps", resource: resource, kind: kind).objectScanMode, .workload, kind)
        }
        // KubeWorkloadsNamed reads no Job, ReplicaSet or CronJob; a CRD named alike is not a workload.
        XCTAssertNil(KubeAPIResource(group: "batch", resource: "jobs", kind: "Job").objectScanMode)
        XCTAssertNil(KubeAPIResource(group: "batch", resource: "cronjobs", kind: "CronJob").objectScanMode)
        XCTAssertNil(KubeAPIResource(group: "apps", resource: "replicasets", kind: "ReplicaSet").objectScanMode)
        XCTAssertNil(KubeAPIResource(group: "example.com", resource: "deployments", kind: "Deployment").objectScanMode)
        XCTAssertNil(KubeAPIResource(resource: "services", kind: "Service").objectScanMode)
    }

    func testObjectScanIDAndLabel() {
        let id = objectScanID(kind: "Deployment", namespace: "shop", name: "web")
        XCTAssertEqual(id, "object:Deployment:shop/web")
        XCTAssertEqual(objectScanLabel(id), "Deployment shop/web")
        XCTAssertNil(objectScanLabel("grafana"))
        XCTAssertNil(objectScanLabel(systemImagesScanID(node: "192.0.2.10")))
        XCTAssertNil(objectScanLabel("object:"))
        XCTAssertNil(objectScanLabel("object:Pod:"))
        XCTAssertNil(systemImagesNode(id))
    }

    func testTargets() {
        let pod = ImageScanTarget.pod(namespace: "shop", name: "web-0")
        XCTAssertEqual(pod.id, "object:Pod:shop/web-0")
        XCTAssertEqual(pod.pods, [RoutePod(namespace: "shop", pod: "web-0")])
        XCTAssertEqual(pod.images, [])
        XCTAssertFalse(pod.byRefs)
        XCTAssertFalse(pod.isEmpty)

        let workload = ImageScanTarget.workload(kind: "StatefulSet", namespace: "shop", name: "db",
                                                images: ["postgres:17", "", "busybox:1.37", "postgres:17"])
        XCTAssertEqual(workload.id, "object:StatefulSet:shop/db")
        XCTAssertEqual(workload.images, ["postgres:17", "busybox:1.37"])
        XCTAssertEqual(workload.pods, [])
        XCTAssertTrue(workload.byRefs)
        XCTAssertTrue(ImageScanTarget.workload(kind: "Deployment", namespace: "shop", name: "web", images: []).isEmpty)

        let app = ImageScanTarget(app: InventoryApp(id: "web", name: "Web", pods: [
            InventoryPod(namespace: "shop", pod: "web-0", node: "n1"),
            InventoryPod(namespace: "shop", pod: "web-0", node: "n2"),
        ]))
        XCTAssertEqual(app, ImageScanTarget(id: "web", name: "Web", pods: [RoutePod(namespace: "shop", pod: "web-0")]))
    }
}
