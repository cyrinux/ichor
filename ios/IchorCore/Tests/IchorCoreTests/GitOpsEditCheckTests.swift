import XCTest
@testable import IchorCore

final class GitOpsEditCheckTests: XCTestCase {
    private func decode(_ json: String) throws -> GitOpsEditCheck {
        try JSONDecoder().decode(GitOpsEditCheck.self, from: Data(json.utf8))
    }

    func testABuildThatChangesOneObject() throws {
        let c = try decode(#"""
        {"sha":"abc","kind":"kustomize","dir":"apps/web","ok":true,"errors":[],
         "diff":{"name":"apps/web/deployment.yaml","resources":[{"group":"apps","version":"v1","kind":"Deployment",
         "namespace":"web","name":"web","change":"changed","diff":"--- a\n+++ b\n"}],"warnings":[]},"unchanged":2}
        """#)
        XCTAssertTrue(c.built)
        XCTAssertEqual(c.resources.map(\.name), ["web"])
        XCTAssertEqual(c.resources.first?.change, .changed)
        XCTAssertEqual(c.unchanged, 2)
    }

    func testAFailedBuildAndAChartAreNotBuilt() throws {
        let failed = try decode(#"{"kind":"kustomize","ok":false,"errors":[{"message":"no matches","path":"apps/web/patch.yaml","line":3}]}"#)
        XCTAssertFalse(failed.built)
        XCTAssertEqual(failed.errors.first?.line, 3)
        XCTAssertTrue(failed.resources.isEmpty)
        XCTAssertTrue(failed.fails)
        XCTAssertFalse(try decode(#"{"kind":"helm","ok":true}"#).built)
        XCTAssertFalse(try decode(#"{"kind":"kustomize","ok":false,"broken":true}"#).fails)
    }

    func testTheFluxSpecQuotesItsPath() throws {
        let spec = GitOpsEditCheck.fluxSpec(path: "./apps/\"web\"")
        let decoded = try JSONDecoder().decode([String: String].self, from: Data(spec.utf8))
        XCTAssertEqual(decoded["path"], "./apps/\"web\"")
    }
}
