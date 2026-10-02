import XCTest
@testable import IchorCore

final class ResourceBrowserTests: XCTestCase {
    private let types = [
        ResourceType(type: "Nodenames.kubernetes.talos.dev", aliases: ["nodename", "nodenames"], namespace: "k8s"),
        ResourceType(type: "MachineConfigs.config.talos.dev", aliases: ["mc", "machineconfig"], namespace: "config", sensitivity: "sensitive"),
        ResourceType(type: "Addresses.net.talos.dev", aliases: ["address"], namespace: "network"),
        ResourceType(type: "links.net.talos.dev", aliases: ["link"], namespace: "network"),
    ]

    func testDecoding() throws {
        let decoded = try TalosJSON.decode([ResourceType].self, from: """
        [{"type":"OSRootSecrets.secrets.talos.dev","aliases":["osrootsecret"],"namespace":"secrets","sensitivity":"sensitive"},
         {"type":"Links.net.talos.dev","aliases":null,"namespace":"network","sensitivity":""}]
        """)
        XCTAssertTrue(decoded[0].isSensitive)
        XCTAssertFalse(decoded[1].isSensitive)
        XCTAssertEqual(decoded[1].aliases, [])
        XCTAssertEqual(decoded[0].shortName, "OSRootSecrets")
        XCTAssertEqual(decoded[0].id, "secrets/OSRootSecrets.secrets.talos.dev")

        let list = try TalosJSON.decode(ResourceItems.self, from: """
        {"items":[{"id":"eth0","namespace":"network","version":"3","phase":"running","updated":1790000000000},
                  {"id":"lo","version":7}],"truncated":true}
        """)
        XCTAssertEqual(list.items, [ResourceItem(id: "eth0", namespace: "network", version: "3", phase: "running", updated: 1_790_000_000_000),
                                    ResourceItem(id: "lo", version: "7")])
        XCTAssertTrue(list.truncated)
        XCTAssertEqual(try TalosJSON.decode(ResourceItems.self, from: #"{"items":null}"#), ResourceItems(items: []))
        XCTAssertEqual(try TalosJSON.decode(ResourceDocument.self, from: #"{"yaml":"a: 1\n"}"#).yaml, "a: 1\n")
    }

    func testSensitivity() {
        XCTAssertFalse(isSensitiveResource(""))
        XCTAssertFalse(isSensitiveResource("  "))
        XCTAssertFalse(isSensitiveResource("non-sensitive"))
        XCTAssertFalse(isSensitiveResource("NonSensitive"))
        XCTAssertTrue(isSensitiveResource("sensitive"))
        XCTAssertTrue(isSensitiveResource("Sensitive"))
    }

    func testGrouping() {
        let groups = groupResourceTypes(types)
        XCTAssertEqual(groups.map(\.namespace), ["config", "k8s", "network"])
        // Case-insensitive order inside a namespace.
        XCTAssertEqual(groups[2].types.map(\.type), ["Addresses.net.talos.dev", "links.net.talos.dev"])
    }

    func testSearch() {
        XCTAssertEqual(groupResourceTypes(types, query: " MC ").flatMap(\.types).map(\.shortName), ["MachineConfigs"])
        XCTAssertEqual(groupResourceTypes(types, query: "net.talos").map(\.namespace), ["network"])
        XCTAssertEqual(groupResourceTypes(types, query: "nodename").map(\.namespace), ["k8s"])
        // The namespace alone is not searched (same as Android).
        XCTAssertEqual(groupResourceTypes(types, query: "k8s"), [])
        XCTAssertEqual(groupResourceTypes(types, query: "zzz"), [])
    }

    func testItemFilter() {
        let items = [ResourceItem(id: "eth0"), ResourceItem(id: "lo"), ResourceItem(id: "ETH1")]
        XCTAssertEqual(filterResourceItems(items, query: "").map(\.id), ["eth0", "lo", "ETH1"])
        XCTAssertEqual(filterResourceItems(items, query: " eth ").map(\.id), ["eth0", "ETH1"])
    }
}
