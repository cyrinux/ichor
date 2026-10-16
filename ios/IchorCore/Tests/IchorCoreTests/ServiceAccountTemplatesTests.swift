import XCTest
@testable import IchorCore

final class ServiceAccountTemplatesTests: XCTestCase {
    private let json = """
    [{"id":"read","yaml":"kind: ServiceAccount","apply":"kubectl apply -f ichor-phone.yaml","kubeconfig":"NS=ichor"},
     {"id":"future","yaml":"","apply":"","kubeconfig":""},
     {"id":"namespace","yaml":"kind: RoleBinding","apply":"kubectl apply","kubeconfig":"NS=ichor"}]
    """

    func testDecodesInOrderAndDropsUnknownIds() throws {
        let templates = try TalosJSON.decode([ServiceAccountTemplate].self, from: json)
        XCTAssertEqual(templates.count, 3)
        let known = knownServiceAccountTemplates(templates)
        XCTAssertEqual(known.map(\.role), [.read, .namespace])
        XCTAssertEqual(known[0].template.apply, "kubectl apply -f ichor-phone.yaml")
        XCTAssertEqual(known[1].template.yaml, "kind: RoleBinding")
    }

    func testUsesKubeTokenOnlyForATokenKubeconfigCluster() {
        XCTAssertTrue(ContextSummary(name: "a", kind: ContextKind.kube, auth: "token").usesKubeToken)
        XCTAssertFalse(ContextSummary(name: "b", kind: ContextKind.kube, auth: "cert").usesKubeToken)
        XCTAssertFalse(ContextSummary(name: "c", kind: ContextKind.kube, auth: "token", signIn: "oidc").usesKubeToken)
        XCTAssertFalse(ContextSummary(name: "d", kind: ContextKind.kube, demo: true, auth: "token").usesKubeToken)
        XCTAssertFalse(ContextSummary(name: "e", auth: "token").usesKubeToken)
    }
}
