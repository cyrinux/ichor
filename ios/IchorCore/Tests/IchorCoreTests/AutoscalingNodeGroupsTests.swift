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
}
