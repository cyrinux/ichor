import XCTest
@testable import IchorCore

final class UpdateProposalPlanTests: XCTestCase {
    // Two rows of demoClusterUpdates() (go/ichorgo/updates_demo.go), cut down.
    private let updatesJSON = """
        {"at":1767323045000,
         "images":[
          {"repo":"ghcr.io/stefanprodan/podinfo","tag":"6.7.0","digest":"","workloads":[],
           "owner":{"tool":"flux","kind":"Kustomization","namespace":"flux-system","name":"podinfo"},
           "current":"6.7.0","patch":"6.7.1","minor":"6.9.2","source":"ghcr.io",
           "edit":{"op":"image","kind":"Deployment","name":"podinfo","container":"podinfo","image":"ghcr.io/stefanprodan/podinfo:6.7.1","path":""}},
          {"repo":"ghcr.io/example-org/worker","tag":"2.3.0","digest":"","workloads":[],"owner":null,
           "current":"2.3.0","patch":"2.3.1","automatedBy":"argocd-image-updater","edit":null}
         ],
         "charts":[
          {"name":"ingress-nginx","version":"4.12.1","repoURL":"https://kubernetes.github.io/ingress-nginx",
           "owner":{"tool":"flux","kind":"HelmRelease","namespace":"ingress-nginx","name":"ingress-nginx"},
           "current":"4.12.1","minor":"4.13.3","source":"kubernetes.github.io",
           "edit":{"op":"path","path":"spec.chart.spec.version","value":"4.13.3"}}
         ],
         "skipped":[],"lookups":{"queried":3,"failed":0}}
        """

    private func sections() throws -> UpdateSections {
        updateSections(try TalosJSON.decode(ClusterUpdates.self, from: updatesJSON))
    }

    func testARowIsSelectedWithItsSmallestStepUnlessSomethingElseBumpsIt() throws {
        let s = try sections()
        XCTAssertEqual(defaultSelection(s.images[0]), "6.7.1")
        XCTAssertEqual(defaultSelection(s.charts[0]), "4.13.3")
        XCTAssertNil(defaultSelection(s.images[1]))
    }

    func testTheSelectionIsSentAsItemsWithTheirVersions() throws {
        let s = try sections()
        let json = updateProposalItemsJSON([
            (row: s.images[0], version: "6.9.2"), (row: s.charts[0], version: "4.13.3"),
        ])
        let items = try XCTUnwrap(
            try JSONSerialization.jsonObject(with: Data(json.utf8)) as? [[String: Any]])
        XCTAssertEqual(items.count, 2)
        XCTAssertEqual(items[0]["version"] as? String, "6.9.2")
        XCTAssertEqual(items[0]["repo"] as? String, "ghcr.io/stefanprodan/podinfo")
        // A chart's own "version" (the running one) gives way to the one picked.
        XCTAssertEqual(items[1]["version"] as? String, "4.13.3")
        XCTAssertEqual(updateProposalItemsJSON([]), "[]")
    }

    func testThePlanDecodesAndItsFilesStage() throws {
        let json = """
            {"source":{"repoURL":"ssh://git@git.example.test/example-org/fleet.git","webURL":"","tracked":"main","revision":"",
               "path":"apps","kind":"kustomize","chart":"","valueFiles":[],"browsable":true,"fluxArtifact":false},
             "baseRef":"main",
             "files":[{"path":"apps/podinfo/deployment.yaml","content":"b","original":"a","fileSha":"5ha",
               "changes":[{"item":"ghcr.io/stefanprodan/podinfo","index":0,"line":17,"before":"image: x:1","after":"image: x:2"}]}],
             "title":"Update 2 dependencies","message":"Update 2 dependencies","branch":"ichor/update-2-dependencies-0001",
             "body":"| Name |","skipped":[{"item":"registry.example.test/coredns","index":1,"reason":"rendered by a chart"}]}
            """
        let plan = try TalosJSON.decode(UpdateProposalPlan.self, from: json)
        XCTAssertEqual(plan.baseRef, "main")
        XCTAssertEqual(plan.source?.repoURL, "ssh://git@git.example.test/example-org/fleet.git")
        XCTAssertEqual(plan.files[0].changes[0].line, 17)
        XCTAssertEqual(plan.skipped[0].index, 1)
        XCTAssertEqual(
            plan.files[0].staged,
            StagedFile(path: "apps/podinfo/deployment.yaml", original: "a", fileSha: "5ha", draft: "b"))
    }
}
