import XCTest
@testable import IchorCore

/// Mirrors the Android CastAIPlansTest: the same wire JSON, the same expectations.
final class CastAIPlansTests: XCTestCase {
    private let hour: Int64 = 3_600_000
    private let now: Int64 = 1_800_000_000_000

    private var json: String {
        """
        {"castai":{"version":"v1","recommendations":[],
         "stuck":[{"node":"edge-aaaa","failures":1,"retrying":true}],
         "plans":[
          {"name":"p4","createdAt":\(now - hour / 10),"mode":"delete-empty","state":"Running","execute":true,"currency":"USD",
           "beforeMonthly":60,"afterMonthly":0,"savingsPercent":100,"clusterMonthly":4800,"clusterNodes":38,
           "removing":[{"name":"edge-aaaa","status":"inProgress","events":[{"at":\(now - hour / 10),"status":"InProgress","description":"Starting deletion process"}]}]},
          {"name":"p3","createdAt":\(now - 2 * hour),"endedAt":\(now - hour),"mode":"full","state":"Failed","execute":true,"currency":"USD",
           "beforeMonthly":318.35,"afterMonthly":177.54,"missedMonthly":98.0,"failureReason":"Timeout","failurePhase":"Deletion","message":"timed out",
           "removing":[{"name":"edge-aaaa","status":"failed"},{"name":"edge-bbbb","status":"success"}],
           "adding":[{"name":"cast-0","status":"success","instanceType":"c7g.2xlarge","spot":true,"priceHourly":0.1608,
                      "events":[{"at":\(now - 2 * hour),"status":"InProgress"},{"at":\(now - 2 * hour + 95_000),"status":"Success"}]}],
           "budgets":[{"nodePool":"edge","allowed":1,"disrupting":0,"nodes":5}]},
          {"name":"p2","createdAt":\(now - 3 * hour),"mode":"full","state":"Done","execute":true,"currency":"USD",
           "beforeMonthly":128.19,"afterMonthly":76.36,"achievedMonthly":51.83},
          {"name":"p1","createdAt":\(now - 4 * hour),"mode":"delete-empty","state":"Done","execute":true,"currency":"USD",
           "beforeMonthly":50,"afterMonthly":0},
          {"name":"p0","createdAt":\(now - 30 * hour),"mode":"full","state":"Done","execute":true,"currency":"USD","beforeMonthly":999},
          {"name":"waiting","createdAt":\(now - hour),"state":"Created","execute":false,"mode":"weird"}]}}
        """
    }

    private var status: CastAIStatus { get throws { try XCTUnwrap(try TalosJSON.decode(DataServices.self, from: json).castai) } }

    func testDecodesPlans() throws {
        let s = try status
        let failed = s.plans[1]
        XCTAssertEqual(failed.planState, .failed)
        XCTAssertEqual(failed.mode, .full)
        XCTAssertEqual(failed.removed, 1)
        XCTAssertEqual(failed.added, 1)
        XCTAssertEqual(failed.removing.first?.status, .failed)
        XCTAssertEqual(failed.plannedMonthly, 140.81, accuracy: 0.001)
        XCTAssertNil(failed.savedMonthly)
        XCTAssertEqual(failed.budgets.first?.exhausted, false)
        XCTAssertEqual(failed.adding.first?.readySeconds, 95)
        XCTAssertEqual(s.plans.last?.planState, .awaitingApproval)
        XCTAssertEqual(s.plans.last?.mode, .other)
        XCTAssertEqual(s.plans[0].removing.first?.status, .inProgress)
        XCTAssertEqual(s.plans[0].removing.first?.events.first?.tone, .moving)
        XCTAssertEqual(s.plans[0].clusterSharePercent ?? 0, 1.25, accuracy: 0.001)
        XCTAssertEqual(s.planCostScale, 999)
    }

    func testSavedIsMeasuredWhenKnownElsePlanned() throws {
        let s = try status
        XCTAssertEqual(s.plans[2].savedMonthly ?? 0, 51.83, accuracy: 0.001)
        XCTAssertEqual(s.plans[3].savedMonthly ?? 0, 50.0, accuracy: 0.001)
    }

    func testSummarizesTheLastDay() throws {
        let s = try status.planSummary(now: now)
        // p0 is older than a day; the waiting plan counts as other.
        XCTAssertEqual(s.savedMonthly, 101.83, accuracy: 0.001)
        // p1 was not measured: the saved total is partly planned.
        XCTAssertTrue(s.savedEstimated)
        // Only the nodes the failed plan left, not its whole planned saving.
        XCTAssertEqual(s.missedMonthly, 98.0, accuracy: 0.001)
        XCTAssertFalse(s.missedEstimated)
        XCTAssertEqual([s.done, s.failed, s.running, s.other], [2, 1, 1, 1])
        XCTAssertEqual(s.currency, "USD")
        XCTAssertEqual(s.clusterNodes, 38)
    }

    func testGroupsWaitingAndRunningFirst() throws {
        let groups = try status.planGroups
        XCTAssertEqual(groups.map(\.state), [.awaitingApproval, .running, .failed, .done])
        XCTAssertEqual(groups.last?.plans.map(\.name), ["p2", "p1", "p0"])
    }

    func testAStuckNodeMakesAWarningNotAnItem() throws {
        XCTAssertEqual(try status.summary, ServiceSummary(total: 0, attention: 0, health: .warning))
    }

    func testPlansErrorIsKeptApart() throws {
        let s = try XCTUnwrap(try TalosJSON.decode(DataServices.self, from: #"{"castai":{"plansError":"forbidden"}}"#).castai)
        XCTAssertEqual(s.plansError, "forbidden")
        XCTAssertEqual(s.error, "")
        XCTAssertEqual(s.summary.health, .ok)
    }

    func testPlanStatesFromCastAIWords() {
        XCTAssertEqual(CastAIPlanState(state: "Canceled", execute: true), .skipped)
        XCTAssertEqual(CastAIPlanState(state: "Expired", execute: false), .skipped)
        XCTAssertEqual(CastAIPlanState(state: "Created", execute: true), .pending)
        XCTAssertEqual(CastAIPlanState(state: "Pending", execute: false), .awaitingApproval)
    }
}
