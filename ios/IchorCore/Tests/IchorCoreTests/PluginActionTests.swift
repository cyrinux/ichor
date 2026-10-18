import XCTest
@testable import IchorCore

final class PluginActionTests: XCTestCase {
    private func item(_ json: String) throws -> PluginActionItem {
        try TalosJSON.decode(PluginActionItem.self, from: json)
    }

    /// A button with every param type: a required string, an int and an enum with defaults, a bool.
    private func resize() throws -> PluginActionItem {
        try item(#"""
        {"id":"resize","label":"Resize","enabled":true,"confirm":"typedName","params":[
          {"name":"reason","label":"Reason","type":"string"},
          {"name":"replicas","label":"Replicas","type":"int","default":3},
          {"name":"tier","label":"Tier","type":"enum","values":["small","large"],"default":"small"},
          {"name":"force","label":"Force","type":"bool","default":false}]}
        """#)
    }

    // MARK: - Button

    func testButtonOnWhenEnabledAndNotRefused() throws {
        let unknown = try item(#"{"id":"a","label":"A","enabled":true}"#)
        XCTAssertNil(unknown.allowed)
        XCTAssertTrue(unknown.usable)
        // Unknown can-I: on, and nothing claimed.
        XCTAssertNil(unknown.note)
        let allowed = try item(#"{"id":"a","label":"A","enabled":true,"allowed":true}"#)
        XCTAssertTrue(allowed.usable)
        let denied = try item(#"{"id":"a","label":"A","enabled":true,"allowed":false}"#)
        XCTAssertFalse(denied.usable)
        XCTAssertEqual(denied.note, .notAllowed)
        let off = try item(#"{"id":"a","label":"A","enabled":false,"disabledReason":"not asleep","allowed":false}"#)
        XCTAssertFalse(off.usable)
        // The plugin's reason first.
        XCTAssertEqual(off.note, .reason("not asleep"))
        let silent = try item(#"{"id":"a","label":"A","enabled":false}"#)
        XCTAssertFalse(silent.usable)
        XCTAssertEqual(silent.note, .unavailable)
        // Go always writes enabled: absent, the server still decides.
        XCTAssertTrue(try item(#"{"id":"a","label":"A"}"#).usable)
    }

    func testConfirmation() {
        XCTAssertEqual(PluginActionConfirm(wire: "none"), .none)
        XCTAssertEqual(PluginActionConfirm(wire: "plain"), .plain)
        XCTAssertEqual(PluginActionConfirm(wire: "typedName"), .typedName)
        // Unknown or absent: ask.
        XCTAssertEqual(PluginActionConfirm(wire: ""), .plain)
        XCTAssertEqual(PluginActionConfirm(wire: "typedname"), .plain)
    }

    func testDefaultsReadAsTheParamType() throws {
        let params = try resize().params
        XCTAssertEqual(params.map(\.defaultValue), [nil, .int(3), .string("small"), .bool(false)])
        XCTAssertEqual(params.map(\.required), [true, false, false, false])
        // A default of another type is no default.
        let odd = try item(#"{"id":"a","label":"A","params":[{"name":"n","label":"N","type":"int","default":"three"}]}"#)
        XCTAssertNil(odd.params[0].defaultValue)
        XCTAssertTrue(odd.params[0].required)
    }

    // MARK: - Form

    func testFormStartsFromTheDefaults() throws {
        let form = PluginActionForm(params: try resize().params)
        XCTAssertEqual(form.text("reason"), "")
        XCTAssertEqual(form.text("replicas"), "3")
        XCTAssertEqual(form.text("tier"), "small")
        XCTAssertFalse(form.flag("force"))
        let on = PluginActionForm(params: [PluginParam(name: "f", label: "F", type: "bool", defaultValue: .bool(true))])
        XCTAssertTrue(on.flag("f"))
    }

    func testRequiredStringBlocksTheForm() throws {
        let params = try resize().params
        let form = PluginActionForm(params: params)
        XCTAssertEqual(form.problem(params[0]), .required)
        XCTAssertFalse(form.isValid)
        XCTAssertNil(form.paramsJSON)
        let filled = form.withText("load test", for: "reason")
        XCTAssertNil(filled.problem(params[0]))
        XCTAssertTrue(filled.isValid)
        // Changes return a new form.
        XCTAssertEqual(form.text("reason"), "")
    }

    func testParamsJSONIsSortedAndTyped() throws {
        let form = PluginActionForm(params: try resize().params)
            .withText("say \"hi\"\n/ok", for: "reason")
            .withText(" 5 ", for: "replicas")
            .withText("large", for: "tier")
            .withFlag(true, for: "force")
        let json = try XCTUnwrap(form.paramsJSON)
        XCTAssertEqual(json, #"{"force":true,"reason":"say \"hi\"\n/ok","replicas":5,"tier":"large"}"#)
        // Valid JSON that reads back the same values.
        let decoded = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(json.utf8)) as? [String: Any])
        XCTAssertEqual(decoded["reason"] as? String, "say \"hi\"\n/ok")
        XCTAssertEqual(decoded["replicas"] as? Int, 5)
    }

    func testEmptyOptionalFieldsAreLeftToTheDefault() throws {
        let form = PluginActionForm(params: try resize().params)
            .withText("why", for: "reason")
            .withText("", for: "replicas")
            .withText("", for: "tier")
        XCTAssertEqual(form.paramsJSON, #"{"force":false,"reason":"why"}"#)
    }

    func testIntValidation() {
        let param = PluginParam(name: "size", label: "Size", type: "int")
        let form = PluginActionForm(params: [param])
        XCTAssertEqual(form.problem(param), .required)
        XCTAssertEqual(form.withText("big", for: "size").problem(param), .notAnInteger)
        XCTAssertEqual(form.withText("1.5", for: "size").problem(param), .notAnInteger)
        XCTAssertEqual(form.withText("99999999999999999999", for: "size").problem(param), .notAnInteger)
        XCTAssertNil(form.withText("-2", for: "size").problem(param))
        XCTAssertEqual(form.withText("-2", for: "size").paramsJSON, #"{"size":-2}"#)
    }

    func testEnumValidation() {
        let param = PluginParam(name: "tier", label: "Tier", type: "enum", values: ["small", "large"])
        let form = PluginActionForm(params: [param])
        XCTAssertEqual(form.problem(param), .required)
        XCTAssertEqual(form.withText("huge", for: "tier").problem(param), .notAChoice)
        XCTAssertEqual(form.withText("large", for: "tier").paramsJSON, #"{"tier":"large"}"#)
    }

    func testStringLimitIsInBytes() {
        let param = PluginParam(name: "note", label: "Note", type: "string", defaultValue: .string(""))
        let form = PluginActionForm(params: [param])
        // A default: empty is a value.
        XCTAssertNil(form.problem(param))
        XCTAssertEqual(form.paramsJSON, #"{"note":""}"#)
        XCTAssertNil(form.withText(String(repeating: "a", count: pluginParamMaxBytes), for: "note").problem(param))
        // 513 two-byte characters: 1,026 bytes.
        XCTAssertEqual(form.withText(String(repeating: "é", count: 513), for: "note").problem(param), .tooLong)
    }

    func testUnknownTypeIsNeverSent() {
        let param = PluginParam(name: "when", label: "When", type: "date")
        let form = PluginActionForm(params: [param])
        XCTAssertEqual(form.problem(param), .unsupported)
        XCTAssertNil(form.paramsJSON)
    }

    func testNoParamsIsEmpty() {
        XCTAssertEqual(PluginActionForm(params: []).paramsJSON, "")
        XCTAssertTrue(PluginActionForm(params: []).isValid)
    }

    // MARK: - Refusals

    func testDisabledIsThePluginsReason() {
        let failure = PluginActionFailure(message: "Kubernetes API (409 ActionDisabled): not asleep")
        XCTAssertEqual(failure, .disabled(reason: "not asleep"))
        XCTAssertTrue(failure.staleObject)
    }

    func testChangedSinceChecked() {
        let failure = PluginActionFailure(message: "Kubernetes API (409 Conflict): SleepInfo weekend changed since it was checked: refresh and try again")
        XCTAssertEqual(failure, .changed(detail: "SleepInfo weekend changed since it was checked: refresh and try again"))
        XCTAssertTrue(failure.staleObject)
    }

    func testParamErrorNamesTheParam() throws {
        // As ActionParams returns it, and inside a 422.
        XCTAssertEqual(PluginActionFailure(message: #"parameter "size": want an integer, got "big""#),
                       .invalidParam(name: "size", detail: #"want an integer, got "big""#))
        XCTAssertEqual(PluginActionFailure(message: #"Kubernetes API (422 Invalid): parameter "tier": is required"#),
                       .invalidParam(name: "tier", detail: "is required"))
        XCTAssertFalse(PluginActionFailure(message: #"parameter "tier": is required"#).staleObject)
        let action = try resize()
        XCTAssertEqual(action.paramLabel("tier"), "Tier")
        // An undeclared param is said by its name.
        XCTAssertEqual(action.paramLabel("size"), "size")
    }

    func testDemoForbiddenAndOther() {
        XCTAssertEqual(PluginActionFailure(message: "this action is unavailable in the demo cluster"), .demo)
        XCTAssertEqual(PluginActionFailure(message: "Kubernetes API: permission denied: sleepinfos is forbidden"),
                       .forbidden(detail: "sleepinfos is forbidden"))
        XCTAssertEqual(PluginActionFailure(message: "Kubernetes API (409 Conflict): the object has been modified"),
                       .other("Kubernetes API (409 Conflict): the object has been modified"))
        XCTAssertEqual(PluginActionFailure(message: "Kubernetes API: not found: weekend"), .other("Kubernetes API: not found: weekend"))
        XCTAssertEqual(PluginActionFailure(message: ""), .other(""))
    }
}
