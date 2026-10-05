import XCTest
@testable import IchorCore

final class IntegrationsTests: XCTestCase {
    func testDecodesCheckedList() throws {
        let json = #"""
        {"checked":true,"items":[
          {"id":"longhorn","name":"Longhorn","icon":"longhorn","website":"https://longhorn.io","groups":["longhorn.io"],"detected":true,"version":"v1beta2"},
          {"id":"garage","name":"Garage","icon":"garage","website":"https://garagehq.deuxfleurs.fr","groups":[],"detected":false}
        ]}
        """#
        let list = try TalosJSON.decode(Integrations.self, from: json)
        XCTAssertTrue(list.checked)
        XCTAssertEqual(list.items.map(\.id), ["longhorn", "garage"])
        XCTAssertEqual(list.items[0].website, URL(string: "https://longhorn.io"))
        XCTAssertEqual(list.items[0].version, "v1beta2")
        XCTAssertTrue(list.items[0].detected)
        XCTAssertEqual(list.items[1].groups, [])
        XCTAssertEqual(list.items[1].version, "")
    }

    func testDefaults() throws {
        let list = try TalosJSON.decode(Integrations.self, from: #"{"items":[{"id":"dragonfly","name":"Dragonfly","icon":""}]}"#)
        XCTAssertFalse(list.checked)
        XCTAssertNil(list.items[0].app.icon)
        XCTAssertFalse(list.items[0].detected)
    }
}
