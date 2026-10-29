import XCTest
@testable import IchorCore

final class HelmRollbackTests: XCTestCase {
    func testDecodesPlan() throws {
        let plan = try TalosJSON.decode(
            HelmRollbackPlan.self,
            from: """
                {"namespace":"apps","name":"site","from":3,"to":2,"fromChart":"site-1.1.0","toChart":"site-1.0.0",
                 "fromAppVersion":"1.1","toAppVersion":"1.0","future":true,
                 "changes":[{"action":"update","kind":"Deployment","namespace":"apps","name":"web","error":""},
                            {"action":"delete","kind":"ClusterRole","name":"site","error":"forbidden","extra":1},
                            {"action":"keep","kind":"PersistentVolumeClaim","namespace":"apps","name":"data"}],
                 "unchanged":4,"fluxOwner":"flux-system/site","blockers":[]}
                """)
        XCTAssertEqual(plan.from, 3)
        XCTAssertEqual(plan.to, 2)
        XCTAssertEqual(plan.toChart, "site-1.0.0")
        XCTAssertEqual(plan.toAppVersion, "1.0")
        XCTAssertEqual(plan.changes.count, 3)
        XCTAssertEqual(plan.changes[0].label, "Deployment apps/web")
        XCTAssertEqual(plan.changes[1].label, "ClusterRole site")
        XCTAssertEqual(plan.changes[1].error, "forbidden")
        XCTAssertEqual(plan.changes[2].error, "")
        XCTAssertEqual(Set(plan.changes.map(\.id)).count, 3)
        XCTAssertEqual(plan.unchanged, 4)
        XCTAssertEqual(plan.fluxOwner, "flux-system/site")
        XCTAssertTrue(plan.canRun)
        XCTAssertTrue(plan.keepsObjects)
    }

    func testBlockersStopIt() throws {
        let plan = try TalosJSON.decode(
            HelmRollbackPlan.self, from: #"{"blockers":["revision 9 not found"],"changes":null}"#)
        XCTAssertFalse(plan.canRun)
        XCTAssertEqual(plan.blockers, ["revision 9 not found"])
        XCTAssertEqual(plan.changes, [])
    }

    func testDecodesEmptyObject() throws {
        let plan = try TalosJSON.decode(HelmRollbackPlan.self, from: "{}")
        XCTAssertEqual(plan, HelmRollbackPlan())
        XCTAssertTrue(plan.canRun)
        XCTAssertFalse(plan.keepsObjects)
        XCTAssertEqual(
            try TalosJSON.decode(HelmRollbackChange.self, from: "{}"),
            HelmRollbackChange(action: "", kind: "", name: ""))
        XCTAssertFalse(plan.force)
        XCTAssertEqual(plan.historyMax, 0)
        XCTAssertEqual(plan.prune, [])
        XCTAssertNil(plan.pruneList)
        XCTAssertEqual(plan.workloads, 0)
        XCTAssertFalse(plan.canWait)
        XCTAssertEqual(plan.hooks, [])
    }

    func testDecodesOptionsFields() throws {
        let plan = try TalosJSON.decode(
            HelmRollbackPlan.self,
            from: """
                {"from":3,"to":1,"force":true,"historyMax":2,"prune":[1,2],"workloads":2,"prune_extra":null,
                 "changes":[{"action":"replace","kind":"Deployment","namespace":"web","name":"site"}]}
                """)
        XCTAssertTrue(plan.force)
        XCTAssertEqual(plan.historyMax, 2)
        XCTAssertEqual(plan.prune, [1, 2])
        XCTAssertEqual(plan.pruneList, "1, 2")
        XCTAssertEqual(plan.workloads, 2)
        XCTAssertTrue(plan.canWait)
        XCTAssertEqual(plan.changes.first?.action, "replace")

        let nulls = try TalosJSON.decode(HelmRollbackPlan.self, from: #"{"prune":null,"workloads":1}"#)
        XCTAssertEqual(nulls.prune, [])
        XCTAssertTrue(nulls.canWait)
    }

    func testOptionsJSON() throws {
        let defaults = HelmRollbackOptions()
        XCTAssertFalse(defaults.force)
        XCTAssertEqual(defaults.historyMax, 10)
        XCTAssertTrue(defaults.wait)

        let json = try HelmRollbackOptions(force: true, historyMax: 2, wait: false).json()
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(json.utf8)) as? [String: Any])
        XCTAssertEqual(object["force"] as? Bool, true)
        XCTAssertEqual(object["historyMax"] as? Int, 2)
        XCTAssertNil(object["wait"], "wait stays in the app")
        XCTAssertEqual(object.count, 2)

        // Only force and history max change the dry run.
        XCTAssertEqual(HelmRollbackOptions(wait: true).planKey, HelmRollbackOptions(wait: false).planKey)
        XCTAssertNotEqual(HelmRollbackOptions(force: true).planKey, HelmRollbackOptions().planKey)
        XCTAssertNotEqual(HelmRollbackOptions(historyMax: 0).planKey, HelmRollbackOptions().planKey)
        XCTAssertEqual(HelmRollbackOptions.historyMaxRange, 0...50)
    }

    func testDecodesHooks() throws {
        let plan = try TalosJSON.decode(
            HelmRollbackPlan.self,
            from: """
                {"from":3,"to":2,"blockers":[],
                 "hooks":[{"name":"site-migrate","kind":"Job","event":"pre-rollback","weight":-5,
                           "deletePolicies":["before-hook-creation","hook-succeeded"],"error":"","extra":1},
                          {"name":"site-notify","kind":"Job","event":"post-rollback"}]}
                """)
        XCTAssertEqual(plan.hooks.count, 2)
        let pre = plan.hooks[0]
        XCTAssertTrue(pre.runsBefore)
        XCTAssertEqual(pre.weight, -5)
        XCTAssertEqual(pre.deletePolicies, ["before-hook-creation", "hook-succeeded"])
        XCTAssertEqual(pre.label { "weight \($0)" }, "Job site-migrate (weight -5)")
        let post = plan.hooks[1]
        XCTAssertFalse(post.runsBefore)
        XCTAssertEqual(post.weight, 0)
        XCTAssertEqual(post.deletePolicies, [])
        XCTAssertEqual(post.error, "")
        XCTAssertEqual(post.label { "weight \($0)" }, "Job site-notify")
        XCTAssertNotEqual(pre.id, post.id)
        XCTAssertTrue(plan.canRun)

        XCTAssertEqual(
            try TalosJSON.decode(HelmRollbackHook.self, from: "{}"),
            HelmRollbackHook(name: "", kind: "", event: ""))
        let none = try TalosJSON.decode(HelmRollbackPlan.self, from: #"{"hooks":null}"#)
        XCTAssertEqual(none.hooks, [])
        XCTAssertTrue(none.canRun)
    }

    func testRefusedHookStopsIt() throws {
        let plan = try TalosJSON.decode(
            HelmRollbackPlan.self,
            from: """
                {"blockers":[],"hooks":[{"name":"site-migrate","kind":"Job","event":"pre-rollback","weight":3,
                                         "error":"jobs.batch is forbidden"}]}
                """)
        XCTAssertFalse(plan.canRun)
        XCTAssertEqual(plan.hooks.first?.label { "w=\($0)" }, "Job site-migrate (w=3)")
    }

    func testHistoryNotPrunedIsADoneRollback() {
        XCTAssertTrue(isHelmHistoryNotPruned("history not pruned: revision 1: secrets is forbidden"))
        XCTAssertTrue(isHelmHistoryNotPruned("Kubernetes API: history not pruned: revision 2: gone"))
        XCTAssertFalse(isHelmHistoryNotPruned("the release changed: reload"))
    }
}
