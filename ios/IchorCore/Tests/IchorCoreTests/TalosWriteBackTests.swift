import XCTest
@testable import IchorCore

final class TalosWriteBackTests: XCTestCase {
    func testDecodesThePatchAndItsChanges() throws {
        let json = #"""
            {"patch":"machine:\n  install:\n    disk: /dev/nvme0n1\n","changes":[
              {"doc":0,"kind":"v1alpha1","path":"machine.network.nameservers","from":["192.0.2.1"],"to":null},
              {"doc":0,"kind":"v1alpha1","path":"machine.install.disk","from":"/dev/sda","to":"/dev/nvme0n1"},
              {"doc":0,"kind":"v1alpha1","path":"machine.kubelet.extraArgs","to":{"max-pods":"150"}},
              {"doc":0,"kind":"v1alpha1","path":"machine.install.wipe","from":false,"to":true}],
              "touchesSecrets":false,"notes":["machine.network.nameservers was removed"]}
            """#
        let patch = try TalosJSON.decode(TalosConfigPatch.self, from: json)
        XCTAssertTrue(patch.changes[0].removed)
        XCTAssertEqual(patch.changes[0].from?.display, #"["192.0.2.1"]"#)
        XCTAssertEqual(patch.firstPath, "machine.install.disk")
        XCTAssertEqual(patch.changes[1].to?.display, "/dev/nvme0n1")
        XCTAssertNil(patch.changes[2].from)
        XCTAssertEqual(patch.changes[2].to?.display, #"{"max-pods":"150"}"#)
        XCTAssertEqual(patch.changes[3].to?.display, "true")
    }

    func testATalosQuickEditGoesToTheCore() throws {
        let plan = TalosWriteBackPlan(
            file: "talos/nodes/cp-1.yaml", patch: "machine:\n  install:\n    disk: /dev/nvme0n1\n",
            skipped: [TalosRepoSkip(path: "machine.time.servers", why: "set in talos/patches/time.yaml")])
        let edit = QuickEdit.talos(hostname: "cp-1", plan: plan, firstPath: "machine.install.disk")
        XCTAssertEqual(quickEditTitle(edit), "Keep cp-1's config change in Git: machine.install.disk")

        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(try edit.json().utf8)) as? [String: Any])
        XCTAssertEqual(Set(object.keys), ["op", "documentIndex", "patch", "hostname", "create", "skipped"])
        XCTAssertEqual(object["op"] as? String, "talos")
        XCTAssertEqual(object["skipped"] as? [String], ["machine.time.servers: set in talos/patches/time.yaml"])
    }
}
