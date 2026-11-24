import XCTest
@testable import IchorCore

final class AutoscalingNodeGroupsTests: XCTestCase {
    /// What go/ichorgo demoNodeGroups() answers, as KubeNodeGroups writes it.
    private let demo: NodeGroupsReport = {
        let json = """
            {"provider":"eks","installed":true,"region":"eu-west-1","cluster":"ichor-demo","error":"","forbidden":"",
             "groups":[
              {"name":"gpu","status":"DEGRADED","desired":2,"min":0,"max":2,"version":"1.34",
               "releaseVersion":"1.34.1-20251016","amiType":"AL2023_x86_64_NVIDIA","capacityType":"ON_DEMAND",
               "instanceTypes":["g6.xlarge"],"diskSize":100,"nodes":0,"atMax":true,
               "issues":[{"code":"AsgInstanceLaunchFailures","message":"Could not launch On-Demand Instances.",
                          "resourceIds":["eks-gpu-a2c9d1e4-asg"]}],"updating":false,"created":1761000000000},
              {"name":"system","status":"ACTIVE","desired":3,"min":2,"max":6,"version":"1.34",
               "releaseVersion":"1.34.1-20251016","amiType":"AL2023_x86_64_STANDARD","capacityType":"ON_DEMAND",
               "instanceTypes":["m7i.xlarge"],"diskSize":50,"nodes":3,"atMax":false,"issues":[],"updating":false,
               "created":1761000000000}
             ]}
            """
        return try! JSONDecoder().decode(NodeGroupsReport.self, from: Data(json.utf8))
    }()

    func testDecodesTheDemoGroups() {
        XCTAssertTrue(demo.installed)
        XCTAssertEqual(demo.provider, "eks")
        XCTAssertEqual(demo.groups.map(\.name), ["gpu", "system"])
        XCTAssertEqual(demo.groups[0].issues.first?.resourceIds, ["eks-gpu-a2c9d1e4-asg"])
        XCTAssertEqual(demo.groups[0].state, .atMax)
        XCTAssertEqual(demo.groups[1].state, .ok)
        XCTAssertEqual(demo.groups[1].created, 1_761_000_000_000)
    }

    func testMissingFieldsTakeTheirDefaults() throws {
        let report = try JSONDecoder().decode(NodeGroupsReport.self, from: Data(#"{"groups":[{"name":"a"}]}"#.utf8))
        XCTAssertFalse(report.installed)
        XCTAssertEqual(report.groups.first?.instanceTypes, [])
        XCTAssertEqual(report.groups.first?.state, .notReady)
    }

    func testTheAutoscalingAnswerNeverCarriesNodeGroups() throws {
        let json = #"{"hpa":{"installed":true},"nodeGroups":{"installed":true}}"#
        let report = try JSONDecoder().decode(AutoscalingReport.self, from: Data(json.utf8))
        XCTAssertFalse(report.nodeGroups.installed)
        XCTAssertTrue(report.withNodeGroups(demo).nodeGroups.installed)
    }

    func testGroupsAtMaxOrWithAnIssueComeFirstInNeedsAttention() {
        let updating = ManagedNodeGroupRow(name: "spot", status: "UPDATING", updating: true)
        let groups = NodeGroupsReport(installed: true, groups: [updating] + demo.groups)
        let problems = AutoscalingReport(nodeGroups: groups).problems
        XCTAssertEqual(problems.map(\.name), ["gpu", "spot"])
        XCTAssertEqual(problems.map(\.group), [.nodeGroups, .nodeGroups])
        XCTAssertEqual(problems[0].state, .atMax)
        XCTAssertEqual(problems[0].message, "Could not launch On-Demand Instances.")
        XCTAssertEqual(problems[1].message, "UPDATING")
    }

    func testTheSearchKeepsGroupsUnderEveryNamespace() {
        let report = AutoscalingReport(nodeGroups: demo)
        XCTAssertEqual(report.filtered(namespace: nil, query: "g6").nodeGroups.groups.map(\.name), ["gpu"])
        XCTAssertEqual(report.filtered(namespace: "shop", query: "").nodeGroups.groups.count, 2)
    }

    func testNodeGroupSizeProblemsMirrorTheGoRefusals() {
        XCTAssertEqual(nodeGroupSizeProblem(desired: 1, min: -1, max: 3), .minNegative)
        XCTAssertEqual(nodeGroupSizeProblem(desired: 0, min: 0, max: 0), .maxTooLow)
        XCTAssertEqual(nodeGroupSizeProblem(desired: 3, min: 4, max: 3), .minAboveMax)
        XCTAssertEqual(nodeGroupSizeProblem(desired: 1, min: 2, max: 4), .desiredOutside)
        XCTAssertEqual(nodeGroupSizeProblem(desired: 5, min: 2, max: 4), .desiredOutside)
        XCTAssertNil(nodeGroupSizeProblem(desired: 0, min: 0, max: 4))
        XCTAssertNil(nodeGroupSizeProblem(desired: 4, min: 2, max: 4))
    }

    func testNodeGroupUpdateStatus() {
        XCTAssertTrue(NodeGroupUpdate(id: "u", status: "InProgress").inProgress)
        XCTAssertNil(NodeGroupUpdate(id: "u", status: "InProgress").failure)
        XCTAssertNil(NodeGroupUpdate(id: "u", status: "Successful").failure)
        let issue = NodeGroupIssue(code: "X", message: "no capacity")
        let failed = NodeGroupUpdate(id: "u", status: "Failed", errors: [issue])
        XCTAssertEqual(failed.failure, "no capacity")
        // No error message: the status itself says it ended.
        XCTAssertEqual(NodeGroupUpdate(id: "u", status: "Cancelled").failure, "Cancelled")
        let lost = NodeGroupUpdate(id: "u", status: "InProgress").unreadable("timeout")
        XCTAssertTrue(lost.unreadable)
        XCTAssertEqual(lost.failure, "timeout")
        XCTAssertEqual(lost.id, "u")
    }

    func testOnlyActiveOrDegradedGroupsChangeSize() {
        XCTAssertTrue(ManagedNodeGroupRow(name: "a", status: "ACTIVE").canChangeSize)
        XCTAssertTrue(ManagedNodeGroupRow(name: "b", status: "DEGRADED", atMax: true).canChangeSize)
        XCTAssertFalse(ManagedNodeGroupRow(name: "c", status: "UPDATING", updating: true).canChangeSize)
        XCTAssertFalse(ManagedNodeGroupRow(name: "d", status: "CREATING").canChangeSize)
    }

    func testClusterVersionAndBehindDecodeWithDefaults() throws {
        XCTAssertEqual(demo.clusterVersion, "")
        XCTAssertFalse(demo.groups[0].behind)
        let json = #"{"clusterVersion":"1.34","groups":[{"name":"gpu","version":"1.33","behind":true}]}"#
        let report = try JSONDecoder().decode(NodeGroupsReport.self, from: Data(json.utf8))
        XCTAssertEqual(report.clusterVersion, "1.34")
        XCTAssertTrue(report.groups[0].behind)
        XCTAssertEqual(report.filtered(query: "gpu").clusterVersion, "1.34")
    }

    func testUpgradeChoices() {
        let behind = ManagedNodeGroupRow(name: "a", status: "ACTIVE", version: "1.33", behind: true)
        XCTAssertEqual(behind.upgradeChoices(clusterVersion: "1.34"), [.kubernetes("1.34"), .latestImage])
        XCTAssertEqual(behind.upgradeChoices(clusterVersion: "1.34").map(\.version), ["1.34", ""])
        // The control plane's version unknown: only the node image.
        XCTAssertEqual(behind.upgradeChoices(clusterVersion: ""), [.latestImage])
        let current = ManagedNodeGroupRow(name: "b", status: "ACTIVE", version: "1.34")
        XCTAssertEqual(current.upgradeChoices(clusterVersion: "1.34"), [.latestImage])
        let custom = ManagedNodeGroupRow(name: "c", status: "ACTIVE", amiType: "CUSTOM", behind: true)
        XCTAssertEqual(custom.upgradeChoices(clusterVersion: "1.34"), [.kubernetes("1.34")])
        let updating = ManagedNodeGroupRow(name: "d", status: "UPDATING", updating: true, behind: true)
        XCTAssertEqual(updating.upgradeChoices(clusterVersion: "1.34"), [])
    }

    func testPodEvictionFailureIsNamed() {
        let eviction = NodeGroupIssue(code: "PodEvictionFailure", message: "Reached max retries", resourceIds: ["ip-1"])
        let failed = NodeGroupUpdate(id: "v", status: "Failed", type: "VersionUpdate", errors: [eviction])
        XCTAssertTrue(failed.versionUpdate)
        XCTAssertEqual(failed.podEvictionFailure?.resourceIds, ["ip-1"])
        XCTAssertNil(NodeGroupUpdate(id: "u", status: "Failed", type: "ConfigUpdate").podEvictionFailure)
    }

    func testDecodesAGKEReportWithAPoolWithoutAutoscaling() throws {
        let json = """
            {"provider":"gke","installed":true,"region":"europe-west1","cluster":"web","error":"","forbidden":"",
             "clusterVersion":"1.34.1-gke.1200000",
             "groups":[
              {"name":"fixed","status":"ERROR","desired":1,"min":0,"max":0,"version":"1.34.1-gke.1200000",
               "amiType":"COS_CONTAINERD","capacityType":"SPOT","instanceTypes":["c3-standard-4"],"nodes":1,
               "issues":[{"code":"GCE_QUOTA_EXCEEDED","message":"Quota CPUS exceeded","resourceIds":[]}]},
              {"name":"zonal","status":"RUNNING","desired":2,"min":1,"max":2,"nodes":2,"atMax":true}
             ]}
            """
        let report = try JSONDecoder().decode(NodeGroupsReport.self, from: Data(json.utf8))
        XCTAssertTrue(report.gke)
        XCTAssertFalse(demo.gke)
        let fixed = report.groups[0]
        let zonal = report.groups[1]
        XCTAssertTrue(fixed.autoscalingOff)
        XCTAssertEqual(fixed.state, .notReady)
        XCTAssertEqual(fixed.issues.first?.code, "GCE_QUOTA_EXCEEDED")
        XCTAssertFalse(zonal.autoscalingOff)
        XCTAssertEqual(zonal.state, .atMax)
        // RUNNING is GKE's healthy status: only the maximum flags the zonal pool.
        let calm = ManagedNodeGroupRow(name: "zonal", status: ManagedNodeGroupRow.running, min: 1, max: 2)
        XCTAssertFalse(calm.needsAttention)
    }
}
