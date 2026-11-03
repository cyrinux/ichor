import XCTest
@testable import IchorCore

final class ImageReportTests: XCTestCase {
    private let inventory = ClusterInventory(apps: [
        InventoryApp(
            id: "web", name: "Web", namespaces: ["apps"], nodes: ["n2", "n1"],
            images: [InventoryImage(repo: "docker.io/library/nginx", tag: "1.27")],
            pods: [
                InventoryPod(
                    namespace: "apps", pod: "web-0", node: "n2",
                    containers: [InventoryContainer(name: "web", image: "nginx:1.27")])
            ]),
        InventoryApp(id: "db", name: "DB", nodes: ["n1", ""]),
    ])

    func testReportNodesAreEveryNodeRunningAnAppOnce() {
        XCTAssertEqual(inventory.reportNodes, ["n1", "n2"])
    }

    func testImageReportInputKeepsWhatGoReads() throws {
        let json = try inventory.imageReportInput()
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(json.utf8)) as? [String: Any])
        let apps = try XCTUnwrap(object["apps"] as? [[String: Any]])
        XCTAssertEqual(apps.first?["id"] as? String, "web")
        let pods = try XCTUnwrap(apps.first?["pods"] as? [[String: Any]])
        XCTAssertEqual(pods.first?["node"] as? String, "n2")
        let containers = try XCTUnwrap(pods.first?["containers"] as? [[String: Any]])
        XCTAssertEqual(containers.first?["image"] as? String, "nginx:1.27")
        let images = try XCTUnwrap(apps.first?["images"] as? [[String: Any]])
        XCTAssertEqual(images.first?["tag"] as? String, "1.27")
    }

    func testNodeImagesInputIsANodeMap() throws {
        let json = try imageReportNodeImages(["n1": [ContainerImage(name: "nginx:1.27", digest: "sha256:a", size: 10)]])
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(json.utf8)) as? [String: [[String: Any]]])
        XCTAssertEqual(object["n1"]?.first?["name"] as? String, "nginx:1.27")
        XCTAssertEqual(object["n1"]?.first?["size"] as? Int, 10)
    }

    func testDecodesAndSumsUnusedBytes() throws {
        let json = """
            {"unpinned":null,"multiTag":[{"repository":"quay.io/cilium/cilium",
            "tags":[{"tag":"v1.18.1","nodes":["n2"],
            "apps":[{"id":"cilium","name":"Cilium","namespace":"kube-system"}]}]}],
            "registries":[],"unusedOnNode":[{"node":"n1","image":"a:1","sizeBytes":10},
            {"node":"n2","image":"b:2","sizeBytes":5}],"summary":{"unused":2}}
            """
        let report = try TalosJSON.decode(ImageReport.self, from: json)
        XCTAssertTrue(report.unpinned.isEmpty)
        XCTAssertEqual(report.multiTag.first?.tags.first?.apps.first?.id, "cilium")
        XCTAssertEqual(report.unusedBytes, 15)
        XCTAssertEqual(report.summary.unused, 2)
    }
}
