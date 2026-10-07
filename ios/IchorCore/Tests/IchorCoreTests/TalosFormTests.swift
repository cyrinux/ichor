import XCTest
@testable import IchorCore

final class TalosFormTests: XCTestCase {
    private func decoded(_ form: TalosForm) throws -> [String: Any] {
        let json = try form.json()
        return try XCTUnwrap(try JSONSerialization.jsonObject(with: Data(json.utf8)) as? [String: Any])
    }

    func testLinesTrimsAndDropsBlanks() {
        XCTAssertEqual(TalosForm.lines(" 10.0.0.1 \n\n10.0.0.2\r\n  \n"), ["10.0.0.1", "10.0.0.2"])
        XCTAssertEqual(TalosForm.lines(""), [])
    }

    func testDirectJSON() throws {
        let form = TalosForm(mode: .direct, name: " lab ", endpoints: "10.0.0.1\n 10.0.0.2 \n", nodes: "\n10.0.0.3\n",
                             ca: "CA\n", crt: "CRT", key: "KEY", omniURL: "https://acme.omni.example.com",
                             cluster: "ignored", identity: "ops@example.com", serviceAccountKey: "c2E=")
        let json = try decoded(form)
        XCTAssertEqual(json["mode"] as? String, "direct")
        XCTAssertEqual(json["name"] as? String, "lab")
        XCTAssertEqual(json["endpoints"] as? [String], ["10.0.0.1", "10.0.0.2"])
        XCTAssertEqual(json["nodes"] as? [String], ["10.0.0.3"])
        XCTAssertEqual(json["ca"] as? String, "CA")
        XCTAssertEqual(json["crt"] as? String, "CRT")
        XCTAssertEqual(json["key"] as? String, "KEY")
        XCTAssertEqual(Set(json.keys), ["mode", "name", "endpoints", "nodes", "ca", "crt", "key"])
        XCTAssertNil(form.serviceAccountKeyToSet)
    }

    func testDirectJSONLeavesOutEmptyNodes() throws {
        let form = TalosForm(mode: .direct, name: "lab", endpoints: "10.0.0.1", ca: "CA", crt: "CRT", key: "KEY")
        XCTAssertNil(try decoded(form)["nodes"])
    }

    func testOmniJSON() throws {
        let form = TalosForm(mode: .omni, name: "prod", endpoints: "10.0.0.1", ca: "CA",
                             omniURL: " https://acme.omni.example.com ", cluster: " prod ", identity: "ops@example.com",
                             serviceAccountKey: " c2E=\n")
        let json = try decoded(form)
        XCTAssertEqual(json["mode"] as? String, "omni")
        XCTAssertEqual(json["omniUrl"] as? String, "https://acme.omni.example.com")
        XCTAssertEqual(json["cluster"] as? String, "prod")
        XCTAssertEqual(json["identity"] as? String, "ops@example.com")
        XCTAssertEqual(Set(json.keys), ["mode", "name", "omniUrl", "cluster", "identity"])
        XCTAssertEqual(form.serviceAccountKeyToSet, "c2E=")
    }

    func testOmniJSONWithoutIdentity() throws {
        let form = TalosForm(mode: .omni, name: "prod", omniURL: "https://acme.omni.example.com", cluster: "prod", identity: "  ")
        XCTAssertNil(try decoded(form)["identity"])
        XCTAssertNil(form.serviceAccountKeyToSet)
    }

    func testCanSubmit() {
        XCTAssertFalse(TalosForm().canSubmit)
        let direct = TalosForm(mode: .direct, name: "lab", endpoints: "10.0.0.1", ca: "CA", crt: "CRT", key: "KEY")
        XCTAssertTrue(direct.canSubmit)
        var missing = direct
        missing.key = " "
        XCTAssertFalse(missing.canSubmit)
        missing = direct
        missing.endpoints = "\n \n"
        XCTAssertFalse(missing.canSubmit)
        missing = direct
        missing.name = ""
        XCTAssertFalse(missing.canSubmit)

        let omni = TalosForm(mode: .omni, name: "prod", omniURL: "https://acme.omni.example.com", cluster: "prod")
        XCTAssertTrue(omni.canSubmit)
        var noCluster = omni
        noCluster.cluster = ""
        XCTAssertFalse(noCluster.canSubmit)
        // The other mode's fields do not count.
        var switched = direct
        switched.mode = .omni
        XCTAssertFalse(switched.canSubmit)
    }
}
