import XCTest
@testable import IchorCore

final class MachineConfigTests: XCTestCase {
    func testSchemaStatusDecoding() throws {
        let ready = try TalosJSON.decode(ConfigSchemaStatus.self, from: #"{"version":"v1.11.2","available":true,"source":"disk"}"#)
        XCTAssertEqual(ready, ConfigSchemaStatus(version: "v1.11.2", available: true, source: "disk"))
        let missing = try TalosJSON.decode(ConfigSchemaStatus.self, from: #"{"version":"v1.11.2","available":false,"reason":"offline"}"#)
        XCTAssertFalse(missing.available)
        XCTAssertNil(missing.source)
        XCTAssertEqual(missing.reason, "offline")
    }

    func testTreeDecoding() throws {
        let tree = try TalosJSON.decode(ConfigTree.self, from: """
        {"schema":true,"documents":[{"index":0,"title":"v1alpha1","node":{"key":"","path":null,"type":"object",
          "addable":[{"key":"debug","type":"boolean","description":"Verbose logs."},{"key":"persist","type":"boolean"}],
          "children":[
            {"key":"machine","path":["machine"],"type":"object","title":"Machine","description":"Machine settings.","children":[
              {"key":"type","path":["machine","type"],"type":"string","value":"controlplane","enum":["controlplane","worker"]},
              {"key":"token","path":["machine","token"],"type":"string","value":"***","redacted":true},
              {"key":"nodeLabels","path":["machine","nodeLabels"],"type":"object","freeKeyType":"string"},
              {"key":"certSANs","path":["machine","certSANs"],"type":"array","itemType":"string","children":[
                {"key":"0","path":["machine","certSANs","0"],"type":"string","value":"192.0.2.10"}]}]}]}},
          {"index":1,"title":"HostnameConfig","node":{"key":"","path":[],"type":"object"}}]}
        """)
        XCTAssertTrue(tree.schema)
        XCTAssertNil(tree.error)
        XCTAssertEqual(tree.documents.map(\.id), [0, 1])
        XCTAssertEqual(tree.documents[1].title, "HostnameConfig")

        let root = tree.documents[0].node
        XCTAssertEqual(root.path, [])
        XCTAssertEqual(root.id, "")
        XCTAssertTrue(root.isContainer)
        XCTAssertEqual(root.addable, [ConfigAddable(key: "debug", type: "boolean", description: "Verbose logs."),
                                      ConfigAddable(key: "persist", type: "boolean")])
        XCTAssertEqual(root.addable?.map(\.id), ["debug", "persist"])

        let machine = try XCTUnwrap(root.children?.first)
        XCTAssertEqual(machine.title, "Machine")
        XCTAssertEqual(machine.description, "Machine settings.")
        let fields = try XCTUnwrap(machine.children)
        XCTAssertEqual(fields.map(\.key), ["type", "token", "nodeLabels", "certSANs"])

        XCTAssertEqual(fields[0].enum, ["controlplane", "worker"])
        XCTAssertEqual(fields[0].value, "controlplane")
        XCTAssertEqual(fields[0].id, "machine\u{1F}type")
        XCTAssertFalse(fields[0].isContainer)
        XCTAssertFalse(fields[0].isRedacted)
        XCTAssertNil(fields[0].children)

        XCTAssertTrue(fields[1].isRedacted)
        XCTAssertEqual(fields[2].freeKeyType, "string")
        XCTAssertNil(fields[2].itemType)

        XCTAssertTrue(fields[3].isContainer)
        XCTAssertEqual(fields[3].itemType, "string")
        XCTAssertEqual(fields[3].children, [ConfigNode(key: "0", path: ["machine", "certSANs", "0"], type: "string", value: "192.0.2.10")])
    }

    func testTreeSyntaxError() throws {
        let tree = try TalosJSON.decode(ConfigTree.self, from: """
        {"schema":false,"documents":null,"error":{"line":12,"message":"did not find expected key"}}
        """)
        XCTAssertEqual(tree.documents, [])
        XCTAssertEqual(tree.error, ConfigSyntaxError(line: 12, message: "did not find expected key"))
    }

    func testPreviewDecoding() throws {
        let preview = try TalosJSON.decode(ConfigPreview.self, from: """
        {"changed":true,"needsReboot":false,"lines":[{"kind":"hunk","text":"@@ -1,3 +1,3 @@"},
          {"kind":"context","text":"machine:"},{"kind":"removed","text":"  type: worker"},
          {"kind":"added","text":"  type: controlplane"},{"kind":"moved","text":"later core"}]}
        """)
        XCTAssertTrue(preview.changed)
        XCTAssertFalse(preview.needsReboot)
        XCTAssertEqual(preview.lines.map(\.id), [0, 1, 2, 3, 4])
        XCTAssertEqual(preview.lines.map(\.kind), [.hunk, .context, .removed, .added, .context])
        XCTAssertEqual(preview.lines[2].text, "  type: worker")

        let unchanged = try TalosJSON.decode(ConfigPreview.self, from: #"{"changed":false,"lines":null,"needsReboot":true}"#)
        XCTAssertEqual(unchanged, ConfigPreview(changed: false, lines: [], needsReboot: true))
    }

    func testProgressDecoding() throws {
        let applying = try TalosJSON.decode(ConfigTryProgress.self, from: #"{"phase":"applying","message":"","deadline":0,"at":1700000000000}"#)
        XCTAssertEqual(applying, ConfigTryProgress(phase: .applying, at: 1_700_000_000_000))
        XCTAssertNil(applying.deadlineDate)

        let trying = try TalosJSON.decode(ConfigTryProgress.self, from: """
        {"phase":"trying","message":"keep failed: timeout","deadline":1700000060500,"at":1700000000000}
        """)
        XCTAssertEqual(trying.phase, .trying)
        XCTAssertEqual(trying.message, "keep failed: timeout")
        XCTAssertEqual(trying.deadlineDate, Date(timeIntervalSince1970: 1_700_000_060.5))

        XCTAssertEqual(try TalosJSON.decode(ConfigTryProgress.self, from: #"{"phase":"keeping"}"#).phase, .keeping)
        XCTAssertEqual(try TalosJSON.decode(ConfigTryProgress.self, from: #"{"phase":"reverting"}"#).phase, .reverting)
        XCTAssertEqual(try TalosJSON.decode(ConfigTryProgress.self, from: #"{"phase":"waiting"}"#).phase, .trying)
    }

    func testEditJSON() throws {
        struct Wire: Decodable, Equatable {
            let doc: Int
            let path: [String]
            let op: String
            let key: String
            let type: String
            let value: String
        }
        func wire(_ edit: ConfigEdit) throws -> Wire { try TalosJSON.decode(Wire.self, from: edit.json()) }

        XCTAssertEqual(try wire(.set(doc: 0, path: ["machine", "type"], type: "string", value: "worker")),
                       Wire(doc: 0, path: ["machine", "type"], op: "set", key: "", type: "string", value: "worker"))
        XCTAssertEqual(try wire(.add(doc: 1, path: ["machine", "nodeLabels"], key: "zone", type: "string", value: "a \"b\"")),
                       Wire(doc: 1, path: ["machine", "nodeLabels"], op: "add", key: "zone", type: "string", value: "a \"b\""))
        XCTAssertEqual(try wire(.add(doc: 0, path: ["machine", "certSANs"], key: "", type: "string", value: "192.0.2.11")),
                       Wire(doc: 0, path: ["machine", "certSANs"], op: "add", key: "", type: "string", value: "192.0.2.11"))
        XCTAssertEqual(try wire(.remove(doc: 2, path: [])),
                       Wire(doc: 2, path: [], op: "remove", key: "", type: "", value: ""))
    }

    func testTryOutcome() {
        XCTAssertEqual(configTryOutcome(outcome: "kept", errMessage: ""), .kept)
        XCTAssertEqual(configTryOutcome(outcome: "reverted", errMessage: ""), .reverted)
        XCTAssertEqual(configTryOutcome(outcome: "", errMessage: "node unreachable"), .failed("node unreachable"))
        XCTAssertEqual(configTryOutcome(outcome: "kept", errMessage: "node unreachable"), .failed("node unreachable"))
        XCTAssertEqual(configTryOutcome(outcome: "", errMessage: ""), .failed(""))
    }

    func testValueTypes() {
        let all = ["string", "integer", "number", "boolean", "object", "array"]
        XCTAssertEqual(configValueTypes(for: "any"), all)
        XCTAssertEqual(configValueTypes(for: ""), all)
        XCTAssertEqual(configValueTypes(for: "null"), all)
        for type in all { XCTAssertEqual(configValueTypes(for: type), [type]) }
    }

    func testSecondsLeft() {
        let now = Date(timeIntervalSince1970: 1_700_000_000)
        XCTAssertEqual(configSecondsLeft(deadline: now.addingTimeInterval(60), now: now), 60)
        XCTAssertEqual(configSecondsLeft(deadline: now.addingTimeInterval(0.2), now: now), 1)
        XCTAssertEqual(configSecondsLeft(deadline: now, now: now), 0)
        XCTAssertEqual(configSecondsLeft(deadline: now.addingTimeInterval(-5), now: now), 0)
        XCTAssertEqual(configTryTimeouts, [60, 300, 600])
    }
}
