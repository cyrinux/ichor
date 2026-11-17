import XCTest
@testable import IchorCore

final class RunbookProposalTests: XCTestCase {
    func testDecodeProposalWithPlanAndRejectedSteps() throws {
        let proposal = try TalosJSON.decode(
            RunbookProposal.self,
            from: #"""
                {"runbookYAML":"schemaVersion: ichor.dev/runbook/v1alpha1\nname: Fix\n","plan":{"name":"Fix","steps":[
                 {"id":"scale-down","action":"scale","target":{"kind":"Deployment","namespace":"demo-shop","name":"ingest-api","node":""},"destructive":true,"role":"os:admin","allowed":true,"denied":""}
                ],"errors":[]},"rejected":[{"id":"shell","reason":"unknown action \"kubectl\""},{"id":"","reason":"not valid YAML"}],"empty":false}
                """#)

        XCTAssertEqual(proposal.runnablePlan?.steps.map(\.id), ["scale-down"])
        XCTAssertEqual(proposal.rejected.map(\.line), [#"shell: unknown action "kubectl""#, "not valid YAML"])
        XCTAssertTrue(proposal.runnablePlan?.canRun(continueIDs: []) ?? false)
    }

    func testEmptyProposalHasNoPlan() throws {
        let proposal = try TalosJSON.decode(
            RunbookProposal.self,
            from: #"{"runbookYAML":"","plan":null,"rejected":[{"id":"shell","reason":"unknown action"}],"empty":true}"#)

        XCTAssertNil(proposal.runnablePlan)
        XCTAssertTrue(proposal.empty)
        XCTAssertEqual(proposal.rejected.count, 1)
    }
}
