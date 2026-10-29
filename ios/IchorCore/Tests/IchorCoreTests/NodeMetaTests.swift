import XCTest
@testable import IchorCore

final class NodeMetaTests: XCTestCase {
    private let readJSON = """
        {"node":"worker-1","resourceVersion":"4242",
        "taints":[{"key":"maintenance","value":"true","effect":"NoSchedule","timeAdded":1700000000,"protected":false},
        {"key":"node-role.kubernetes.io/control-plane","effect":"NoSchedule","timeAdded":0,"protected":true},
        {"key":"node.kubernetes.io/unschedulable","effect":"NoSchedule","protected":true}],
        "labels":[{"key":"example.com/pool","value":"general","protected":false},
        {"key":"topology.kubernetes.io/zone","value":"zone-a","protected":true}]}
        """

    private func read() throws -> KubeNodeMeta {
        try TalosJSON.decode(KubeNodeMeta.self, from: readJSON)
    }

    func testDecodesTheRead() throws {
        let meta = try read()
        XCTAssertEqual(meta.node, "worker-1")
        XCTAssertEqual(meta.resourceVersion, "4242")
        XCTAssertEqual(
            meta.taints.map(\.display),
            [
                "maintenance=true:NoSchedule", "node-role.kubernetes.io/control-plane:NoSchedule",
                "node.kubernetes.io/unschedulable:NoSchedule",
            ])
        XCTAssertEqual(meta.taints[0].timeAdded, 1_700_000_000)
        XCTAssertEqual(meta.taints[1].value, "")
        XCTAssertTrue(meta.taints[2].isCordon)
        XCTAssertEqual(meta.labels.map(\.protected), [false, true])
        XCTAssertEqual(try TalosJSON.decode(KubeNodeMeta.self, from: #"{"node":"n"}"#), KubeNodeMeta(node: "n"))
    }

    func testUntouchedDraftHasNoEdits() throws {
        let draft = NodeMetaDraft(try read())
        XCTAssertFalse(draft.hasChanges)
        XCTAssertFalse(draft.touchesProtected)
        let edits = draft.edits
        XCTAssertEqual(
            edits.taints,
            [
                NodeTaintEdit(key: "maintenance", value: "true", effect: "NoSchedule"),
                NodeTaintEdit(key: "node-role.kubernetes.io/control-plane", effect: "NoSchedule"),
            ], "the full list, the cordon's taint left out")
        XCTAssertTrue(edits.setLabels.isEmpty)
        XCTAssertTrue(edits.removeLabels.isEmpty)
    }

    func testRemovingTheStrayTaintIsATaintChange() throws {
        let meta = try read()
        var draft = NodeMetaDraft(meta)
        draft.removeTaint(meta.taints[0])
        XCTAssertTrue(draft.hasTaintChanges)
        XCTAssertFalse(draft.hasLabelChanges)
        XCTAssertFalse(draft.touchesProtected)
        XCTAssertEqual(
            try draft.edits.json(),
            """
            {"removeLabels":[],"resourceVersion":"4242","setLabels":{},"taints":[{"effect":"NoSchedule","key":"node-role.kubernetes.io/control-plane","value":""}]}
            """)
    }

    func testLabelEditsAreDeltas() throws {
        let meta = try read()
        var draft = NodeMetaDraft(meta)
        draft.setLabel(NodeLabel(key: "example.com/pool", value: "batch"))
        draft.setLabel(NodeLabel(key: "workload", value: "web"))
        XCTAssertTrue(draft.hasLabelChanges)
        XCTAssertFalse(draft.hasTaintChanges)
        XCTAssertEqual(draft.edits.setLabels, ["example.com/pool": "batch", "workload": "web"])
        // Labels only: the read's version and the current taints (no cordon's) still go along.
        XCTAssertEqual(draft.edits.resourceVersion, "4242")
        XCTAssertEqual(draft.edits.taints.map(\.key), ["maintenance", "node-role.kubernetes.io/control-plane"])
        XCTAssertTrue(try draft.edits.json().contains(#""resourceVersion":"4242""#))
        XCTAssertEqual(draft.labels.map(\.key), ["example.com/pool", "topology.kubernetes.io/zone", "workload"])
        XCTAssertFalse(draft.touchesProtected)

        draft.removeLabel(meta.labels[1])
        XCTAssertEqual(draft.edits.removeLabels, ["topology.kubernetes.io/zone"])
        XCTAssertTrue(draft.touchesProtected)
        XCTAssertTrue(try draft.edits.forcing(true).json().contains(#""force":true"#))
        XCTAssertFalse(try draft.edits.json().contains("force"))
        draft.reset()
        XCTAssertFalse(draft.hasChanges)
    }

    func testEditingATaintReplacesIt() throws {
        let meta = try read()
        var draft = NodeMetaDraft(meta)
        draft.setTaint(NodeTaint(key: "maintenance", value: "true", effect: "NoExecute"), replacing: meta.taints[0])
        XCTAssertEqual(draft.taints.filter { $0.key == "maintenance" }.map(\.effect), ["NoExecute"])
        XCTAssertTrue(draft.hasTaintChanges)
        // Same key and effect: one taint, the new value.
        draft.setTaint(NodeTaint(key: "maintenance", value: "later", effect: "NoExecute"))
        XCTAssertEqual(draft.taints.filter { $0.key == "maintenance" }.map(\.value), ["later"])
        // Changing a protected one needs force.
        draft.setTaint(
            NodeTaint(key: "node-role.kubernetes.io/control-plane", value: "x", effect: "NoSchedule"),
            replacing: meta.taints[1])
        XCTAssertTrue(draft.taints.first { $0.key == "node-role.kubernetes.io/control-plane" }?.protected ?? false)
        XCTAssertTrue(draft.touchesProtected)
    }

    func testEditableRules() throws {
        let meta = try read()
        XCTAssertTrue(NodeMetaDraft.editable(meta.taints[0], showProtected: false))
        XCTAssertFalse(NodeMetaDraft.editable(meta.taints[1], showProtected: false))
        XCTAssertTrue(NodeMetaDraft.editable(meta.taints[1], showProtected: true))
        XCTAssertFalse(
            NodeMetaDraft.editable(meta.taints[2], showProtected: true), "the cordon's taint is Cordon / Uncordon's")
        XCTAssertFalse(NodeMetaDraft.editable(meta.labels[1], showProtected: false))
        XCTAssertTrue(NodeMetaDraft.editable(meta.labels[1], showProtected: true))
        XCTAssertEqual(NodeTaintEffect.allCases.map(\.rawValue), ["NoSchedule", "PreferNoSchedule", "NoExecute"])
    }

    func testDecodesThePlan() throws {
        let plan = try TalosJSON.decode(
            NodeMetaPlan.self,
            from: """
                {"node":"worker-1",
                "labels":{"added":[{"key":"workload","value":"web"}],"removed":[],"changed":[{"key":"example.com/pool","from":"general","to":"batch"}]},
                "taints":{"added":[{"key":"maintenance","value":"true","effect":"NoExecute"}],"removed":[{"key":"maintenance","value":"true","effect":"NoSchedule"}]},
                "evicts":[{"namespace":"shop","name":"web-1","owner":"ReplicaSet/web-5d9"}],
                "blockers":[],"warnings":["1 pods will be evicted at once"],"noChange":false}
                """)
        XCTAssertEqual(plan.labels.added.map(\.display), ["workload=web"])
        XCTAssertEqual(plan.labels.changed.map(\.display), ["example.com/pool: general → batch"])
        XCTAssertEqual(plan.taints.added.map(\.display), ["maintenance=true:NoExecute"])
        XCTAssertEqual(plan.taints.changed, [])
        XCTAssertEqual(plan.evicts.map(\.id), ["shop/web-1"])
        XCTAssertTrue(plan.hasTaintChanges)
        XCTAssertTrue(plan.canApply)
    }

    func testPlanBlockersAndNoChange() throws {
        let blocked = try TalosJSON.decode(
            NodeMetaPlan.self,
            from: """
                {"node":"worker-1","labels":{"added":[{"key":"kubernetes.io/bad","value":"x"}]},
                "blockers":["metadata.labels: Invalid value"]}
                """)
        XCTAssertFalse(blocked.hasTaintChanges)
        XCTAssertFalse(blocked.canApply)
        XCTAssertFalse(blocked.isStale)
        let stale = try TalosJSON.decode(
            NodeMetaPlan.self,
            from: """
                {"node":"worker-1","taints":{"changed":[{"key":"maintenance","effect":"NoSchedule","from":"true","to":"later"}]},
                "blockers":["the node changed since it was read, reload"],"warnings":["the node changed since it was read, reload"]}
                """)
        XCTAssertTrue(stale.isStale)
        XCTAssertEqual(stale.taints.changed.map(\.display), ["maintenance:NoSchedule: true → later"])
        let none = try TalosJSON.decode(NodeMetaPlan.self, from: #"{"node":"worker-1","noChange":true}"#)
        XCTAssertTrue(none.labels.isEmpty)
        XCTAssertFalse(none.canApply)
        XCTAssertEqual(none.evicts, [])
    }
}
