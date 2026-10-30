import XCTest
@testable import IchorCore

final class RunbooksTests: XCTestCase {
    private let planJSON = #"""
        {"name":"Restart ingestion","description":"Scale down, restart.","steps":[
         {"id":"scale-down","action":"scale","target":{"kind":"Deployment","namespace":"demo-shop","name":"ingest-api","node":""},"destructive":true,"continueOnError":false,"role":"os:admin","allowed":false,"denied":"the cluster's RBAC refuses patch deployments.apps in namespace demo-shop"},
         {"id":"compact","action":"triggerCronJob","target":{"kind":"","namespace":"demo-shop","name":"store-compaction","node":""},"destructive":false,"continueOnError":true,"role":"os:admin","allowed":false,"denied":"no"},
         {"id":"reboot","action":"nodeReboot","target":{"kind":"","namespace":"","name":"","node":"10.5.0.5"},"destructive":true,"continueOnError":false,"role":"os:operator","allowed":true,"denied":""}
        ],"errors":[]}
        """#

    func testDecodePlanAndRunRules() throws {
        let plan = try TalosJSON.decode(RunbookPlan.self, from: planJSON)
        XCTAssertEqual(plan.name, "Restart ingestion")
        XCTAssertEqual(plan.steps.count, 3)
        XCTAssertEqual(plan.destructiveCount, 2)
        XCTAssertEqual(plan.steps[0].target.label, "Deployment demo-shop/ingest-api")
        XCTAssertEqual(plan.steps[1].target.label, "demo-shop/store-compaction")
        XCTAssertEqual(plan.steps[2].target.label, "10.5.0.5")

        // The refused scale stops the run; the refused CronJob does not (continueOnError).
        XCTAssertEqual(plan.blocking(continueIDs: []).map(\.id), ["scale-down"])
        XCTAssertFalse(plan.canRun(continueIDs: []))
        XCTAssertTrue(plan.canRun(continueIDs: ["scale-down"]))
        XCTAssertFalse(plan.canRun(continueIDs: ["reboot"]))

        XCTAssertEqual(plan.steps[0].confirmation, .plain)
        XCTAssertNil(plan.steps[1].confirmation)
        XCTAssertEqual(plan.steps[2].confirmation, .typedHostname("10.5.0.5"))
        XCTAssertEqual(runbookContinueIDs(["scale-down", "compact"]), "compact,scale-down")
    }

    func testPlanWithErrorsOrNoStepsCannotRun() throws {
        let broken = try TalosJSON.decode(
            RunbookPlan.self, from: #"{"name":"x","steps":[{"id":"a","action":"scale"}],"errors":["boom"]}"#)
        XCTAssertFalse(broken.canRun(continueIDs: []))
        let empty = try TalosJSON.decode(RunbookPlan.self, from: #"{"name":"x"}"#)
        XCTAssertFalse(empty.canRun(continueIDs: []))
        // Missing fields: allowed by default, as Go answers for a step it could not ask about.
        XCTAssertTrue(broken.steps[0].allowed)
    }

    func testDecodeStoreAndSummary() throws {
        let list = try TalosJSON.decode(
            RunbookList.self,
            from: #"""
                {"items":[{"id":"restart-ingestion","name":"Restart ingestion","description":"d","steps":2,"destructive":2,"importedAt":1792668615147},
                          {"id":"old","name":"old","steps":0,"destructive":0,"importedAt":1,"problem":"step a: unknown action"}]}
                """#)
        XCTAssertEqual(list.items.map(\.id), ["restart-ingestion", "old"])
        XCTAssertTrue(list.items[0].usable)
        XCTAssertFalse(list.items[1].usable)
        XCTAssertEqual(list.items[0].destructive, 2)

        let summary = try TalosJSON.decode(
            RunbookSummary.self, from: #"{"name":"Three steps","description":"","steps":3,"destructive":3}"#)
        XCTAssertEqual(summary.steps, 3)
        XCTAssertEqual(summary.destructive, 3)
    }

    func testProgressFollowsTheStates() throws {
        let plan = try TalosJSON.decode(RunbookPlan.self, from: planJSON)
        var progress = RunbookProgress(plan: plan)
        XCTAssertNil(progress.position)
        XCTAssertTrue(progress.steps.allSatisfy { $0.state == .pending })

        progress.apply(
            try TalosJSON.decode(
                RunbookStepState.self,
                from: #"{"index":0,"id":"scale-down","state":"waiting","message":"","startedAt":0,"endedAt":0}"#))
        XCTAssertEqual(progress.waiting?.id, "scale-down")
        XCTAssertEqual(progress.position?.step, 1)
        XCTAssertEqual(progress.position?.total, 3)

        progress.apply(RunbookStepState(index: 0, id: "scale-down", state: .ok, message: "scaled to 0."))
        progress.apply(RunbookStepState(index: 1, id: "compact", state: .running))
        XCTAssertNil(progress.waiting)
        XCTAssertEqual(progress.position?.step, 2)
        XCTAssertEqual(progress.ended, 1)

        // Out of range and unknown states do not break the run's screen.
        progress.apply(RunbookStepState(index: 9, id: "x", state: .ok))
        let unknown = try TalosJSON.decode(
            RunbookStepState.self, from: #"{"index":2,"id":"reboot","state":"something-new"}"#)
        XCTAssertEqual(unknown.state, .pending)

        let result = try TalosJSON.decode(
            RunbookResult.self,
            from: #"""
                {"ok":false,"steps":[{"index":0,"id":"scale-down","state":"ok"},{"index":1,"id":"compact","state":"failed","message":"forbidden"},{"index":2,"id":"reboot","state":"skipped","message":"step compact failed"}]}
                """#)
        progress.apply(result)
        XCTAssertFalse(result.ok)
        XCTAssertEqual(progress.ended, 3)
        XCTAssertEqual(progress.steps[2].state, .skipped)
        XCTAssertEqual(progress.position?.step, 3)
    }

    func testDecodeParamsAndWhatTheFormNeeds() throws {
        let json = """
            {"name":"r","steps":1,"params":[
              {"name":"ns","prompt":"Namespace","default":"","kind":"namespace"},
              {"name":"app","default":"ingest-api"},
              {"name":"old","default":"x"}]}
            """
        let summary = try JSONDecoder().decode(RunbookSummary.self, from: Data(json.utf8))
        XCTAssertEqual(summary.params.map(\.name), ["ns", "app", "old"])
        XCTAssertEqual(summary.params[0].kind, RunbookParam.kindNamespace)
        XCTAssertEqual(summary.params[1].prompt, "app", "no prompt: the name")
        XCTAssertEqual(summary.params[1].kind, "string")

        let values = RunbookParam.initialValues(summary.params, remembered: ["ns": "demo-shop", "gone": "y", "old": " "])
        XCTAssertEqual(values, ["ns": "demo-shop", "app": "ingest-api", "old": "x"])
        XCTAssertEqual(RunbookParam.missing(summary.params, values: values), [])
        XCTAssertEqual(RunbookParam.missing(summary.params, values: ["ns": " "]), ["ns"])
        XCTAssertEqual(RunbookParam.label(["ns": "demo-shop", "app": "api"]), "app=api · ns=demo-shop")
    }

    func testASummaryWithoutParamsAsksNothing() throws {
        let summary = try JSONDecoder().decode(RunbookSummary.self, from: Data(#"{"name":"r"}"#.utf8))
        XCTAssertEqual(summary.params, [])
    }

    func testRunbookOperationKind() {
        XCTAssertEqual(OperationButton.offered(for: .runbook), [.stop])
        XCTAssertEqual(OperationButton.offered(for: .runbook, stopping: true), [])
        XCTAssertTrue(OperationKind.runbook.hasScreen)
        XCTAssertEqual(OperationKind(link: OperationKind.runbook.link), .runbook)
    }
}
