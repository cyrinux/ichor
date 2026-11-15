import XCTest

@testable import IchorCore

final class NewClusterTests: XCTestCase {
    private let start = NewClusterStart(draftId: "d1", name: "lab", talosconfig: "context: lab\n    key: c2VjcmV0\n")

    func testNewClusterRunHasItsOwnSteps() {
        let request = JoinRequest(
            address: "192.0.2.10", template: "", role: "controlplane", hostname: "lab", disk: "/dev/vda",
            staticAddress: "", newCluster: start)
        XCTAssertEqual(request.phases, JoinPhase.newClusterRun)
        XCTAssertEqual(request.withReplacing(false).newCluster, start)
        XCTAssertFalse(request.cloud)
    }

    func testTimelineFollowsTheGoPhases() throws {
        let events = try ["secrets", "applying", "installing", "booted", "bootstrapping"].enumerated().map { i, phase in
            try TalosJSON.decode(JoinProgress.self, from: #"{"phase":"\#(phase)","message":"","at":\#(i)}"#)
        }
        let steps = joinTimeline(events, phases: JoinPhase.newClusterRun)
        XCTAssertEqual(steps.map(\.phase), JoinPhase.newClusterRun)
        XCTAssertEqual(steps.last?.phase, .ready)
        XCTAssertEqual(JoinPhase(rawValue: "ready"), .ready)
    }

    func testDraftDecodesAndHidesItsCredential() throws {
        let draft = try TalosJSON.decode(
            NewClusterDraft.self,
            from: """
                {"draftId":"d1","talosconfig":"secret-yaml","redactedControlplane":"x","warnings":["w"],"size":3,
                 "talosVersion":"v1.14.2","kubernetesVersion":"1.37.1"}
                """
        )
        XCTAssertEqual(draft.talosconfig, "secret-yaml")
        XCTAssertFalse(draft.description.contains("secret-yaml"))
        XCTAssertFalse(start.description.contains("c2VjcmV0"))
    }

    func testSpecEncodesTheGoKeys() throws {
        let spec = NewClusterSpec(name: "lab", endpoint: "192.0.2.10", talosVersion: "v1.14.2", installDisk: "/dev/vda")
        let json = String(decoding: try JSONEncoder().encode(spec), as: UTF8.self)
        for key in ["\"name\"", "\"endpoint\"", "\"installDisk\"", "\"allowSchedulingOnControlPlanes\":true"] {
            XCTAssertTrue(json.contains(key), json)
        }
    }
}
