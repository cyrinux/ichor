import XCTest
@testable import IchorCore

final class AlertFlapsTests: XCTestCase {
    func testDecodesGoFlapsAndMapsRingKeysToAlertKeys() throws {
        let json = #"""
            {"windowHours":24,"alerts":[
              {"key":"longhorn|vol-1","track":"data","opens":5,"closes":4,"open":true,"lastOpenedAt":1780000000000},
              {"key":"abc","track":"am","opens":3,"closes":3,"open":false,"lastOpenedAt":1779990000000},
              {"key":"cert","track":"cert","opens":2,"closes":1,"open":true,"lastOpenedAt":0},
              {"key":"NOSPACE","track":"etcd","opens":2}]}
            """#
        let flaps = try TalosJSON.decode(HistoryFlaps.self, from: json)
        XCTAssertEqual(flaps.alerts.first?.track, "data")
        XCTAssertEqual(
            flaps.opensByAlertKey, ["data:longhorn|vol-1": 5, "am:abc": 3, "cert": 2, "etcd:NOSPACE": 2])
        XCTAssertEqual(try TalosJSON.decode(HistoryFlaps.self, from: "{}"), HistoryFlaps())
    }

    func testRuleFlapOpensOverride() {
        let rules = AlertRules(
            default: AlertRule(flapOpens: 3),
            clusters: ["fp-lab": AlertRule(flapOpens: 0), "fp-qa": AlertRule(digest: true)])
        XCTAssertEqual(rules.flapOpens("fp-prod"), 3)
        XCTAssertEqual(rules.flapOpens("fp-lab"), 0)
        XCTAssertEqual(rules.flapOpens("fp-qa"), 3)
        XCTAssertEqual(AlertRules.none.flapOpens("fp-prod"), 0)
        XCTAssertFalse(AlertRule(flapOpens: 0).isEmpty)
    }

    func testRulesRoundTripTheFlapOpens() throws {
        let rules = AlertRules(default: AlertRule(flapOpens: 4))
        XCTAssertTrue(rules.json.contains(#""flapOpens":4"#))
        XCTAssertEqual(AlertRules.decode(Data(rules.json.utf8)), rules)
        XCTAssertFalse(AlertRules.none.json.contains("flapOpens"))
    }

    func testDecisionsAndDigestsCarryTheFlaps() throws {
        let json = #"""
            {"decisions":[{"cluster":"fp","key":"am:abc","action":"digest","reason":"flapping","until":0,"repeats":false,"flaps":5}],
             "digests":[{"cluster":"fp","count":1,"critical":0,"warning":1,"firstKeys":["am:abc"],"flapping":1,"flappingKeys":["am:abc"]}]}
            """#
        let decided = try TalosJSON.decode(AlertDecisions.self, from: json)
        XCTAssertEqual(decided.decisions.first?.reason, alertReasonFlapping)
        XCTAssertEqual(decided.decisions.first?.flaps, 5)
        XCTAssertEqual(decided.digests.first?.flappingKeys, ["am:abc"])
        let input = AlertInput(cluster: "fp", key: "am:abc", kind: "am", problem: true, flaps: 4)
        let encoded = String(decoding: try JSONEncoder().encode(input), as: UTF8.self)
        XCTAssertTrue(encoded.contains(#""flaps":4"#))
    }
}
