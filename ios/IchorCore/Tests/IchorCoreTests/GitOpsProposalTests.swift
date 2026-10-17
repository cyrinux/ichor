import XCTest
@testable import IchorCore

final class GitOpsProposalTests: XCTestCase {
    private func source(tracked: String, webURL: String = "https://github.com/example-org/platform",
                        browsable: Bool = true) -> GitSource {
        GitSource(repoURL: "https://github.com/example-org/platform.git", webURL: webURL, tracked: tracked,
                  revision: "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b", path: "apps/podinfo", browsable: browsable)
    }

    func testBaseRefIsTheTrackedBranch() {
        XCTAssertEqual(proposalBaseRef(for: source(tracked: "main")), "main")
        XCTAssertEqual(proposalBaseRef(for: source(tracked: "release/2024-q3")), "release/2024-q3")
        XCTAssertEqual(proposalBaseRef(for: source(tracked: "refs/heads/staging")), "staging")
        XCTAssertEqual(proposalBaseRef(for: source(tracked: " main ")), "main")
    }

    func testBaseRefAsksForTagsCommitsAndRanges() {
        for tracked in ["", "HEAD", "v1.2.3", "1.2.3", "v2", "1.x", "v1.2.3-rc.1", "refs/tags/v1.0.0", "refs/pull/1/head",
                        "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b", ">=1.0.0", "~1.2", "*", "a..b", "-main"] {
            XCTAssertNil(proposalBaseRef(for: source(tracked: tracked)), tracked)
        }
    }

    func testEditNeedsAForgeAndAToken() {
        XCTAssertNil(sourceEditBlock(source(tracked: "main"), demo: false, hasToken: true))
        XCTAssertEqual(sourceEditBlock(source(tracked: "main"), demo: false, hasToken: false), .noToken(host: "github.com"))
        XCTAssertNil(sourceEditBlock(source(tracked: "main"), demo: true, hasToken: false))
        XCTAssertEqual(sourceEditBlock(source(tracked: "main", browsable: false), demo: false, hasToken: true), .noForge)
        XCTAssertEqual(sourceEditBlock(source(tracked: "main", webURL: ""), demo: true, hasToken: true), .noForge)
        // A Flux source read from its artifact still proposes through its forge.
        let flux = GitSource(repoURL: "https://github.com/example-org/platform", webURL: "https://github.com/example-org/platform",
                             tracked: "main", browsable: true, fluxArtifact: true,
                             fluxSource: FluxRef(kind: "GitRepository", namespace: "flux-system", name: "flux-system"))
        XCTAssertEqual(flux.proposalHost, "github.com")
    }

    func testProposalOptionsJSON() throws {
        let proposal = GitProposal(repoURL: "https://github.com/example-org/platform.git", baseRef: "main",
                                   path: "apps/podinfo/deployment.yaml", content: "kind: Deployment\n", fileSha: "f11e5ha",
                                   message: "Update apps/podinfo/deployment.yaml", branch: "ichor/podinfo-deployment-1a2b",
                                   title: "Update apps/podinfo/deployment.yaml", body: "Proposed from Ichor.", draft: true)
        let json = try proposal.optionsJSON()
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(json.utf8)) as? [String: Any])
        XCTAssertEqual(Set(object.keys), ["repoURL", "baseRef", "path", "content", "fileSha", "message", "branch", "title", "body", "draft"])
        XCTAssertEqual(object["fileSha"] as? String, "f11e5ha")
        XCTAssertEqual(object["draft"] as? Bool, true)
        XCTAssertEqual(try TalosJSON.decode(GitProposal.self, from: json), proposal)
    }

    func testDecodesResultAndDefaults() throws {
        let result = try TalosJSON.decode(GitProposalResult.self, from: #"""
        {"url":"https://github.com/example-org/platform/pull/42","number":42,"branch":"ichor/podinfo-deployment-1a2b",
         "commit":"9f8e7d6","forge":"github"}
        """#)
        XCTAssertEqual(result, GitProposalResult(url: "https://github.com/example-org/platform/pull/42", number: 42,
                                                 branch: "ichor/podinfo-deployment-1a2b", commit: "9f8e7d6", forge: "github"))
        XCTAssertEqual(try TalosJSON.decode(GitProposalResult.self, from: #"{"url":"u"}"#).number, 0)

        let defaults = try TalosJSON.decode(GitProposalDefaults.self, from: #"""
        {"title":"Update apps/podinfo/deployment.yaml","message":"Update apps/podinfo/deployment.yaml",
         "branch":"ichor/podinfo-deployment-1a2b","body":"Proposed from Ichor for Argo CD Application argocd/podinfo."}
        """#)
        XCTAssertEqual(defaults.branch, "ichor/podinfo-deployment-1a2b")
        XCTAssertEqual(GitOpsAppRef.argo(namespace: "argocd", name: "podinfo").toolLabel, "Argo CD")
        XCTAssertEqual(GitOpsAppRef.flux(kind: "Kustomization", namespace: "flux-system", name: "apps").toolLabel, "Flux")
    }

    func testDecodesTheTextDiff() throws {
        let diff = try TalosJSON.decode(FluxDiff.self, from: #"""
        {"kind":"file","name":"apps/podinfo/deployment.yaml","resources":[{"kind":"file","name":"apps/podinfo/deployment.yaml",
         "change":"changed","diff":"--- a/x\n+++ b/x\n@@ -1,2 +1,2 @@\n kind: Deployment\n-replicas: 1\n+replicas: 2\n+# more\n"}],
         "warnings":[]}
        """#)
        let file = try XCTUnwrap(diff.resources.first)
        XCTAssertEqual(file.change, .changed)
        XCTAssertEqual(file.counts.added, 2)
        XCTAssertEqual(file.counts.removed, 1)
    }

    func testTellsAStaleFile() {
        XCTAssertTrue(isStaleProposal("the file changed on main since you opened it: reload and try again"))
        XCTAssertFalse(isStaleProposal("this action is unavailable in the demo cluster"))
        XCTAssertTrue(sameSourceText("a\r\nb\n", "a\nb\n"))
        XCTAssertFalse(sameSourceText("a\nb", "a\nc"))
    }

    func testManifestTypeForSchemaHelp() {
        XCTAssertEqual(sourceManifestType("apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: podinfo\n"),
                       SourceManifestType(group: "apps", version: "v1", kind: "Deployment"))
        XCTAssertEqual(sourceManifestType("---\napiVersion: v1\nkind: \"ConfigMap\" # app settings\n---\napiVersion: v1\nkind: ConfigMap\n"),
                       SourceManifestType(group: "", version: "v1", kind: "ConfigMap"))
        // Documents of different kinds, a values file, a kustomization: no schema help.
        XCTAssertNil(sourceManifestType("apiVersion: v1\nkind: Service\n---\napiVersion: apps/v1\nkind: Deployment\n"))
        XCTAssertNil(sourceManifestType("replicaCount: 2\nimage:\n  tag: 6.7.1\n"))
        XCTAssertNil(sourceManifestType("apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources: []\n"))
        // A nested kind is not the document's.
        XCTAssertNil(sourceManifestType("spec:\n  kind: Deployment\n  apiVersion: apps/v1\n"))
    }
}
