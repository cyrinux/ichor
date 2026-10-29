import XCTest
@testable import IchorCore

final class TalosRepoTests: XCTestCase {
    func testDecodesTheFilesAndPicksThePatches() throws {
        let json = #"""
            {"sha":"abc","generator":"talhelper","files":[
              {"path":"talos/talconfig.yaml","kind":"talconfig","why":"talconfig.yaml lists cp-1","line":7,"patch":false},
              {"path":"talos/patches/network.yaml","kind":"shared","why":"talconfig.yaml patches","patch":true},
              {"path":"talos/talconfig.yaml#L12","kind":"talconfig","why":"inline patch","line":12,"patch":true},
              {"path":"talos/nodes/cp-1.yaml","kind":"node","why":"talconfig.yaml nodes[cp-1].patches","patch":true},
              {"path":"talos/talsecret.sops.yaml","kind":"secrets","why":"encrypted with SOPS","patch":false},
              {"path":"x.yaml","kind":"future"}],"note":""}
            """#
        let files = try TalosJSON.decode(TalosRepoFiles.self, from: json)
        XCTAssertEqual(files.files[4].kind, .secrets)
        XCTAssertEqual(files.files[5].kind, .shared)
        XCTAssertEqual(
            files.defaultSelection, ["talos/patches/network.yaml", "talos/talconfig.yaml#L12", "talos/nodes/cp-1.yaml"])
        XCTAssertEqual(files.files[2].file, "talos/talconfig.yaml")
        // The order is the files', never the order they were picked in; secrets never go.
        XCTAssertEqual(
            files.ordered(["talos/nodes/cp-1.yaml", "talos/talsecret.sops.yaml", "talos/patches/network.yaml"]),
            ["talos/patches/network.yaml", "talos/nodes/cp-1.yaml"])
    }

    func testDecodesTheComparison() throws {
        let json = #"""
            {"sha":"abc","files":["a.yaml"],"diff":{"kind":"file","name":"machine config","resources":[{"kind":"file","name":"machine config","change":"changed","diff":"-a\n+b\n"}],"warnings":[]},
             "same":false,"needsReboot":true,"skipped":[{"path":"b.yaml","why":"is not YAML"}],"draft":"version: v1alpha1\n"}
            """#
        let compare = try TalosJSON.decode(TalosRepoCompare.self, from: json)
        XCTAssertFalse(compare.same)
        XCTAssertTrue(compare.needsReboot)
        XCTAssertEqual(compare.diff.resources.first?.diff, "-a\n+b\n")
        XCTAssertEqual(compare.skipped.map(\.path), ["b.yaml"])
    }

    func testReadsTheRoleFromMachineType() {
        XCTAssertEqual(
            configRole(
                "version: v1alpha1\nmachine:\n  network:\n    hostname: cp-1\n  type: controlplane\ncluster:\n  type: other\n"
            ), "controlplane")
        XCTAssertEqual(configRole("machine:\n  type: init\n"), "controlplane")
        XCTAssertEqual(configRole("machine:\n  type: \"worker\"\n"), "worker")
        XCTAssertEqual(configRole("machine:\n  install:\n    type: disk\ncluster:\n  type: x\n"), "")
    }
}
