import XCTest
@testable import IchorCore

final class SensorAlertsTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)

    private func worker(_ celsius: Double, throttled: Bool = false) -> SensorNode {
        SensorNode(
            node: "192.0.2.20", hostname: "worker-1",
            hottest: TemperatureSensor(
                chip: "coretemp", label: "Package id 0", celsius: celsius, critCelsius: 100, maxCelsius: 85),
            throttled: throttled)
    }

    private func health(_ nodes: SensorNode...) -> ClusterSensorHealth {
        ClusterSensorHealth(context: "lab", nodes: nodes)
    }

    private func snap(_ issues: [String: String]?, watched: Bool = true) -> ClusterSnapshot {
        ClusterSnapshot(
            context: "lab", takenAt: now, nodes: ["192.0.2.20": NodeState(hostname: "worker-1", health: .ready)],
            etcdChecked: true, sensorWatched: watched, sensorChecked: watched && issues != nil,
            sensorIssues: issues ?? [:])
    }

    func testTheGoJSONDecodes() throws {
        let decoded = try TalosJSON.decode(
            ClusterSensorHealth.self,
            from: """
                {"context":"lab","nodes":[
                  {"node":"192.0.2.20","hostname":"worker-1","hottest":{"chip":"coretemp","label":"Package id 0","celsius":96,"critCelsius":100,"maxCelsius":85},"throttled":true},
                  {"node":"192.0.2.21","hostname":"vm-1","hottest":null,"throttled":false},
                  {"node":"192.0.2.30","hostname":"192.0.2.30","hottest":null,"throttled":false,"error":"connection refused"}]}
                """)
        XCTAssertNil(decoded.nodes[1].hottest)
        XCTAssertEqual(
            Set(sensorIssuesOf(decoded, threshold: sensorThresholdAuto).keys),
            ["192.0.2.20|hot", "192.0.2.20|throttled"])
    }

    func testTheLimit() {
        let sensor = TemperatureSensor(critCelsius: 100, maxCelsius: 85)
        XCTAssertEqual(sensorLimit(sensor, threshold: sensorThresholdAuto), 95)
        XCTAssertEqual(sensorLimit(TemperatureSensor(maxCelsius: 85), threshold: sensorThresholdAuto), 85)
        XCTAssertNil(sensorLimit(TemperatureSensor(celsius: 50), threshold: sensorThresholdAuto))
        XCTAssertEqual(sensorLimit(sensor, threshold: 70), 70)
        XCTAssertEqual(sensorLimit(sensor, threshold: 10), 60)
    }

    func testHotWithHysteresis() {
        XCTAssertTrue(sensorIssuesOf(health(worker(94.9)), threshold: sensorThresholdAuto).isEmpty)
        let hot = sensorIssuesOf(health(worker(95)), threshold: sensorThresholdAuto)
        let issue = SensorIssue(value: hot["192.0.2.20|hot"] ?? "")
        XCTAssertEqual(issue.severity, dataCritical)
        XCTAssertEqual(issue.label, "Package id 0")
        XCTAssertEqual(issue.limit, 95)
        XCTAssertEqual(
            sensorIssuesOf(health(worker(92.5)), threshold: sensorThresholdAuto, known: hot).keys.sorted(),
            hot.keys.sorted())
        XCTAssertTrue(sensorIssuesOf(health(worker(92)), threshold: sensorThresholdAuto, known: hot).isEmpty)
    }

    func testAHotNodeAlertsAtOnceThenOnceCooled() {
        let first = evaluate(
            previous: snap([:]), current: snap(sensorIssuesOf(health(worker(97)), threshold: sensorThresholdAuto)),
            now: now)
        XCTAssertEqual(first.alerts.map(\.key), ["sensor:192.0.2.20|hot"])
        XCTAssertTrue(first.alerts[0].problem)
        XCTAssertEqual(
            ShareTarget.forAlert(key: first.alerts[0].key, snapshot: first.next),
            .hardware(address: "192.0.2.20", hostname: "worker-1"))
        let still = evaluate(
            previous: first.next,
            current: snap(
                sensorIssuesOf(health(worker(98)), threshold: sensorThresholdAuto, known: first.next.sensorIssues)),
            now: now)
        XCTAssertTrue(still.alerts.isEmpty)
        let cooled = evaluate(
            previous: still.next,
            current: snap(
                sensorIssuesOf(health(worker(60)), threshold: sensorThresholdAuto, known: still.next.sensorIssues)),
            now: now)
        XCTAssertEqual(cooled.alerts.map(\.problem), [false])
        XCTAssertEqual(cooled.alerts[0].title, "worker-1 back to a normal temperature")
    }

    func testThrottlingAlertsOnTheSecondCheck() {
        let throttled = sensorIssuesOf(health(worker(60, throttled: true)), threshold: sensorThresholdAuto)
        let once = evaluate(previous: snap([:]), current: snap(throttled), now: now)
        XCTAssertTrue(once.alerts.isEmpty)
        let twice = evaluate(previous: once.next, current: snap(throttled), now: now)
        XCTAssertEqual(twice.alerts.map(\.title), ["worker-1 is throttled (60.0 °C)"])
    }

    func testAThrottledNodeWithoutAReadingHasNoValue() {
        let bare = SensorNode(node: "192.0.2.20", hostname: "worker-1", throttled: true)
        let issue = SensorIssue(
            value: sensorIssuesOf(health(bare), threshold: sensorThresholdAuto)["192.0.2.20|throttled"]!)
        XCTAssertEqual(issue.celsius, 0)
        XCTAssertEqual(sensorTitle(issue, problem: true), "worker-1 is throttled")
        // A value stored before the reading was kept.
        XCTAssertEqual(
            sensorTitle(SensorIssue(value: "warning|throttled|worker-1||0.0|0.0"), problem: true),
            "worker-1 is throttled")
    }

    func testOldSnapshotsDecodeWithoutTheSensorFields() throws {
        let data = try JSONEncoder().encode(snap(["192.0.2.20|hot": "critical|hot|worker-1|x|96.0|95.0"]))
        var object = try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
        for key in ["sensorWatched", "sensorChecked", "sensorIssues", "sensorPending"] { object[key] = nil }
        let decoded = try JSONDecoder().decode(
            ClusterSnapshot.self, from: JSONSerialization.data(withJSONObject: object))
        XCTAssertFalse(decoded.sensorWatched)
        XCTAssertTrue(decoded.sensorIssues.isEmpty)
    }

    func testTheIssueRoundTrips() {
        let issue = SensorIssue(
            severity: dataCritical, kind: SensorIssue.hot, hostname: "worker|1", label: "Package id 0", celsius: 96.04,
            limit: 95)
        let back = SensorIssue(value: issue.value)
        XCTAssertEqual(back.hostname, "worker/1")
        XCTAssertEqual(back.celsius, 96)
        XCTAssertEqual(back.limit, 95)
    }
}
