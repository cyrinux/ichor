import XCTest
@testable import IchorCore

final class ExplainSubjectTests: XCTestCase {
    private func object(_ subject: ExplainSubject) throws -> [String: Any] {
        let json = try subject.json()
        return try XCTUnwrap(JSONSerialization.jsonObject(with: Data(json.utf8)) as? [String: Any])
    }

    func testPodAndAlert() throws {
        let pod = try object(.pod(namespace: "shop", name: "cart-1"))
        XCTAssertEqual(pod["namespace"] as? String, "shop")
        XCTAssertEqual(pod["name"] as? String, "cart-1")
        XCTAssertEqual(ExplainSubject.pod(namespace: "a", name: "b").kind, "pod")

        let source = #"{"mode":"proxy","kind":"alertmanager","namespace":"monitoring","service":"am","port":9093}"#
        let alert = try object(.alert(sourceJSON: source, fingerprint: "fp1"))
        // The source as a JSON text, which Go reads as it reads AlertmanagerAlerts' argument.
        XCTAssertEqual(alert["source"] as? String, source)
        XCTAssertEqual(alert["fingerprint"] as? String, "fp1")
    }

    func testFindingKeepsGoNames() throws {
        let finding = try TalosJSON.decode(
            CheckupFinding.self,
            from: """
                {"kind":"pvcPending","severity":"warning","namespace":"shop",
                 "name":"PersistentVolumeClaim/data","reason":"ProvisioningFailed"}
                """)
        let subject = try object(.finding(finding, text: "A volume claim waits", fix: "Check the storage class"))
        let wire = try XCTUnwrap(subject["finding"] as? [String: Any])
        XCTAssertEqual(wire["kind"] as? String, "pvcPending")
        XCTAssertEqual(wire["severity"] as? String, "warning")
        XCTAssertEqual(wire["name"] as? String, "PersistentVolumeClaim/data")
        XCTAssertEqual(subject["text"] as? String, "A volume claim waits")
        XCTAssertEqual(subject["fix"] as? String, "Check the storage class")
    }

    func testNotes() {
        XCTAssertEqual(
            podExplainNote(namespace: "shop", name: "cart-1", status: ""), "Why is pod shop/cart-1 not running?")
        XCTAssertEqual(
            alertExplainNote(alertname: "KubePodCrashLooping"), "Why is the alert KubePodCrashLooping firing?")
        XCTAssertEqual(findingExplainNote(text: "x"), "The checkup found: x")
    }
}
