import XCTest
@testable import IchorCore

final class ConfigMultiTests: XCTestCase {
    func testPreviewKeepsWhatWouldChange() throws {
        let preview = try TalosJSON.decode(MultiConfigPreview.self, from: """
        {"nodes":[
          {"node":"10.0.0.1","hostname":"cp-1","changed":true,"lines":[{"kind":"added","text":"a: b"}],"needsReboot":false},
          {"node":"10.0.0.2","hostname":"","changed":false,"lines":null,"needsReboot":false,"error":"machine.nodeLabels: not found"},
          {"node":"10.0.0.3","hostname":"w-1","changed":true,"lines":[],"needsReboot":true}
        ],"anyReboot":true}
        """)
        XCTAssertEqual(preview.changing.map(\.name), ["cp-1", "w-1"])
        XCTAssertEqual(preview.nodes[1].name, "10.0.0.2")
        XCTAssertEqual(preview.nodes[0].lines.first?.kind, .added)
        XCTAssertEqual(preview.applyModes, [.staged, .reboot])
    }

    func testProgressStates() throws {
        let progress = try TalosJSON.decode(MultiConfigProgress.self, from: """
        {"phase":"applying","message":"","at":1,"index":1,"total":2,"node":"10.0.0.1",
         "nodes":[{"node":"10.0.0.3","hostname":"w-1","state":"done"},{"node":"10.0.0.1","state":"later"}]}
        """)
        XCTAssertEqual(progress.total, 2)
        XCTAssertEqual(progress.nodes.map(\.nodeState), [.done, .pending])
        XCTAssertEqual(progress.nodes[1].name, "10.0.0.1")
    }
}
