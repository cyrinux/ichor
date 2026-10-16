import XCTest
@testable import IchorCore

final class PinsTests: XCTestCase {
    private let node = Pin(target: .node(address: "10.0.0.11", hostname: "worker-1", tab: ""), label: "worker-1")
    private let api = Pin(target: .workload(kind: "Deployment", namespace: "shop", name: "api"), label: "api")
    private let app = Pin(target: .argoApp(namespace: "argocd", name: "shop"), label: "shop")

    func testEncodeRoundTripsInAndroidForm() {
        let list = PinList([node, api])
        XCTAssertEqual(list.encoded, """
        [{"label":"worker-1","target":{"addr":"10.0.0.11","host":"worker-1","target":"node"}},\
        {"label":"api","target":{"kind":"Deployment","name":"api","ns":"shop","target":"workload"}}]
        """)
        XCTAssertEqual(PinList.parse(list.encoded), list)
    }

    func testParseReadsAndroidJSON() {
        let text = #"[{"target":{"target":"flux-app","kind":"HelmRelease","ns":"web","name":"site"},"label":"site"}]"#
        let list = PinList.parse(text)
        XCTAssertEqual(list.pins.map(\.target), [.fluxApp(kind: "HelmRelease", namespace: "web", name: "site")])
        XCTAssertEqual(list.pins.map(\.label), ["site"])
    }

    func testParseSkipsUnknownTargetsAndFallsBackToEmpty() {
        let text = #"[{"target":{"target":"someday"},"label":"x"},{"target":{"target":"pod","ns":"a","name":"b"},"label":"b"}]"#
        XCTAssertEqual(PinList.parse(text).pins.map(\.target), [.pod(namespace: "a", name: "b")])
        XCTAssertTrue(PinList.parse(nil).isEmpty)
        XCTAssertTrue(PinList.parse("").isEmpty)
        XCTAssertTrue(PinList.parse("garbage").isEmpty)
        XCTAssertEqual(PinList().encoded, "")
    }

    func testClusterIsLeftOut() {
        var target = ShareTarget.cronJob(namespace: "jobs", name: "backup")
        target.cluster = "c1u5t3r"
        let pin = Pin(target: target, label: "backup")
        XCTAssertEqual(pin.target.cluster, "")
        XCTAssertTrue(PinList([pin]).contains(target))
        XCTAssertFalse(PinList([pin]).encoded.contains("c1u5t3r"))
    }

    func testToggleAddsThenRemoves() {
        let added = PinList([node]).toggling(app)
        XCTAssertEqual(added.pins, [node, app])
        // Same screen, another label: still that pin.
        let removed = added.toggling(Pin(target: app.target, label: "renamed"))
        XCTAssertEqual(removed.pins, [node])
        XCTAssertEqual(removed.removing(node.target), PinList())
    }

    func testAtMostTwelve() {
        let many = (0..<PinList.limit).map { Pin(target: .pod(namespace: "ns", name: "p\($0)"), label: "p\($0)") }
        let full = PinList(many)
        XCTAssertTrue(full.isFull)
        XCTAssertEqual(full.toggling(api), full)
        // Unpinning still works when full.
        XCTAssertEqual(full.toggling(many[0]).pins.count, PinList.limit - 1)
        XCTAssertEqual(PinList(many + [api]).pins.count, PinList.limit)
        XCTAssertEqual(PinList.parse(PinList(many + [api, app]).encoded).pins.count, PinList.limit)
    }

    func testDuplicatesAreDropped() {
        XCTAssertEqual(PinList([node, node, api]).pins, [node, api])
        // The same node pinned from one of its tabs: still that pin.
        XCTAssertTrue(PinList([node]).contains(.node(address: "10.0.0.11", hostname: "worker-1", tab: "services")))
    }

    func testParseSkipsWhatCannotBePinned() {
        let text = #"[{"target":{"target":"etcd"},"label":"etcd"},{"target":{"target":"pod","ns":"a","name":"b"},"label":"b"}]"#
        XCTAssertEqual(PinList.parse(text).pins.map(\.label), ["b"])
    }

    func testStorageKeyAndPinnable() {
        XCTAssertEqual(PinList.storageKey(fingerprint: "ab12"), "pins.ab12")
        XCTAssertEqual(Pin.pinnable, [.node, .workload, .pod, .cronJob, .argoApp, .fluxApp])
    }
}
