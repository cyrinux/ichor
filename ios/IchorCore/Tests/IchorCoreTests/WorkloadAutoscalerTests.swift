import XCTest
@testable import IchorCore

final class WorkloadAutoscalerTests: XCTestCase {
    func testLabelNamesTheAutoscalerWithItsBounds() {
        let hpa = WorkloadAutoscaler(by: "hpa", name: "web-hpa", min: 1, max: 5, current: 3, desired: 3, state: "ok")
        XCTAssertEqual(hpa.label, "HPA web-hpa · 3 of 1–5")
        XCTAssertTrue(hpa.stateOk)
    }

    func testLabelNamesTheScaledObjectAndItsPause() {
        let keda = WorkloadAutoscaler(by: "keda", name: "worker-so", min: 0, max: 10, current: 0, desired: 0, paused: true, state: "paused")
        XCTAssertEqual(keda.label, "KEDA worker-so · paused at 0")
        XCTAssertFalse(keda.stateOk)
    }

    func testDecodesFromTheGoCoreAndToleratesItsAbsence() throws {
        let with = try JSONDecoder().decode(KubeWorkload.self, from: Data(
            #"{"kind":"Deployment","namespace":"shop","name":"web","autoscaler":{"by":"keda","name":"worker-so","min":0,"max":10,"current":2,"desired":2,"paused":true,"state":"paused"}}"#.utf8))
        XCTAssertEqual(with.autoscaler?.label, "KEDA worker-so · paused at 2")

        let without = try JSONDecoder().decode(KubeWorkload.self, from: Data(
            #"{"kind":"Deployment","namespace":"shop","name":"plain"}"#.utf8))
        XCTAssertNil(without.autoscaler)
    }
}
