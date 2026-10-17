import XCTest
@testable import IchorCore

final class GitOpsSourceTests: XCTestCase {
    private let sample = #"""
    {"sources":[
      {"repoURL":"https://github.com/example-org/platform.git","webURL":"https://github.com/example-org/platform",
       "tracked":"main","revision":"1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b","path":"./apps/podinfo/","kind":"kustomize",
       "chart":"","valueFiles":[],"browsable":true,"fluxArtifact":false},
      {"repoURL":"https://charts.example.org","webURL":"","tracked":"6.5.0","revision":"","path":"","kind":"chart",
       "chart":"podinfo","valueFiles":["$values/podinfo.yaml"],"browsable":false,"fluxArtifact":false},
      {"repoURL":"https://github.com/example-org/values.git","webURL":"https://github.com/example-org/values",
       "tracked":"main","revision":"","path":"","kind":"values","browsable":true,"fluxArtifact":false}
    ],"note":""}
    """#

    func testDecodesTheSources() throws {
        let decoded = try TalosJSON.decode(GitOpsSource.self, from: sample)
        XCTAssertEqual(decoded.sources.count, 3)
        let git = decoded.sources[0]
        XCTAssertEqual(git.kind, .kustomize)
        XCTAssertEqual(git.host, "github.com")
        XCTAssertNil(git.fluxSource)
        XCTAssertEqual(decoded.sources[1].valueFiles, ["$values/podinfo.yaml"])
        XCTAssertEqual(decoded.rows.map(\.index), [0, 1])

        let flux = try TalosJSON.decode(GitOpsSource.self, from: #"""
        {"sources":[{"repoURL":"ssh://git@git.example.org/platform","kind":"newer","fluxArtifact":true,
          "fluxSource":{"kind":"GitRepository","namespace":"flux-system","name":"flux-system"}}]}
        """#)
        XCTAssertEqual(flux.sources[0].kind, .directory)
        XCTAssertEqual(flux.sources[0].fluxSource, FluxRef(kind: "GitRepository", namespace: "flux-system", name: "flux-system"))
        XCTAssertEqual(flux.note, "")
    }

    func testChoosesHowToRead() {
        let flux = FluxRef(kind: "GitRepository", namespace: "flux-system", name: "flux-system")
        XCTAssertEqual(GitSource(repoURL: "r", webURL: "https://github.com/a/b", revision: "abc", browsable: true,
                                 fluxArtifact: true, fluxSource: flux).read, .flux(flux))
        // An artifact without its source: the forge, when it can.
        XCTAssertEqual(GitSource(repoURL: "r", webURL: "https://github.com/a/b", tracked: "main", browsable: true,
                                 fluxArtifact: true).read, .forge(host: "github.com", ref: "main"))
        XCTAssertEqual(GitSource(repoURL: "r", webURL: "https://GitLab.example.org:8443/g/p", tracked: "main", revision: "abc",
                                 browsable: true).read, .forge(host: "gitlab.example.org:8443", ref: "abc"))
        XCTAssertEqual(GitSource(repoURL: "r", webURL: "", browsable: true).read, .unreadable)
        XCTAssertEqual(GitSource(repoURL: "r", webURL: "https://github.com/a/b").read, .unreadable)

        let chart = GitSource(repoURL: "https://charts.example.org", kind: .chart, chart: "podinfo")
        XCTAssertTrue(chart.isChart)
        XCTAssertFalse(chart.canOpen)
        XCTAssertFalse(GitSource(repoURL: "r", kind: .values).hasRow)
        XCTAssertTrue(GitSource(repoURL: "r", webURL: "https://github.com/a/b", browsable: true).canOpen)
    }

    func testRowLabel() {
        let sha = "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b"
        XCTAssertEqual(sourceRowLabel(GitSource(repoURL: "r", tracked: "main", revision: sha, path: "apps/podinfo")), "apps/podinfo @ 1a2b3c4")
        XCTAssertEqual(sourceRowLabel(GitSource(repoURL: "r", tracked: "main", path: "./apps/podinfo/")), "apps/podinfo @ main")
        XCTAssertEqual(sourceRowLabel(GitSource(repoURL: "r", revision: sha, path: "./")), "/ @ 1a2b3c4")
        XCTAssertEqual(sourceRowLabel(GitSource(repoURL: "r", path: "apps")), "apps")
    }

    func testBreadcrumb() {
        XCTAssertEqual(sourceBreadcrumb(""), [])
        XCTAssertEqual(sourceBreadcrumb("./apps//podinfo/"), [
            SourceCrumb(name: "apps", path: "apps"),
            SourceCrumb(name: "podinfo", path: "apps/podinfo"),
        ])
        XCTAssertEqual(GitSource(repoURL: "r", path: "./clusters/home/").startDir, "clusters/home")
        XCTAssertEqual(cleanSourcePath("."), "")
    }

    func testDecodesTreeAndFile() throws {
        let tree = try TalosJSON.decode(GitTree.self, from: #"""
        {"ref":"main","sha":"abc","entries":[{"path":"apps/podinfo/kustomization.yaml","type":"file","size":120},
          {"path":"apps/podinfo/base","type":"dir","size":0}]}
        """#)
        XCTAssertEqual(tree.sorted.map(\.name), ["base", "kustomization.yaml"])
        XCTAssertTrue(tree.sorted[0].isDir)
        XCTAssertEqual(tree.sorted[1].size, 120)

        let bare = try TalosJSON.decode(GitTree.self, from: #"{"ref":"main","entries":[]}"#)
        XCTAssertEqual(bare.sha, "")

        let file = try TalosJSON.decode(GitFile.self, from: #"{"path":"values.yaml","sha":"f00","size":2097152,"content":"","truncated":true}"#)
        XCTAssertTrue(file.truncated)
        XCTAssertEqual(file.size, 2_097_152)
    }

    func testWebPages() {
        let repo = "https://github.com/example-org/platform"
        XCTAssertEqual(gitWebURL(repo, kind: .auto, ref: "abc", path: "apps/podinfo/deployment.yaml", dir: false),
                       "https://github.com/example-org/platform/blob/abc/apps/podinfo/deployment.yaml")
        XCTAssertEqual(gitWebURL(repo + ".git", kind: .github, ref: "main", path: "", dir: true),
                       "https://github.com/example-org/platform/tree/main")
        XCTAssertEqual(gitWebURL("https://gitlab.example.org/g/p/", kind: .auto, ref: "main", path: "apps", dir: true),
                       "https://gitlab.example.org/g/p/-/tree/main/apps")
        XCTAssertEqual(gitWebURL("https://git.example.org/o/r", kind: .gitea, ref: "1a2b3c4d5e6f7a8b", path: "a.yaml", dir: false),
                       "https://git.example.org/o/r/src/commit/1a2b3c4d5e6f7a8b/a.yaml")
        XCTAssertEqual(gitWebURL("https://git.example.org/o/r", kind: .gitea, ref: "main", path: "a.yaml", dir: false),
                       "https://git.example.org/o/r/src/branch/main/a.yaml")
        XCTAssertEqual(gitWebURL(repo, kind: .auto, ref: "", path: "a", dir: false), repo)
        XCTAssertEqual(gitWebURL("", kind: .auto, ref: "main", path: "a", dir: false), "")
    }

    func testDecodesSourceFind() throws {
        let found = try TalosJSON.decode(GitOpsSourceFind.self, from: #"""
        {"matches":[{"path":"apps/podinfo/deployment.yaml","line":12,"documentIndex":0}],"searched":17,"note":""}
        """#)
        XCTAssertEqual(found.searched, 17)
        XCTAssertEqual(found.outcome, .open(ManifestMatch(path: "apps/podinfo/deployment.yaml", line: 12)))

        let several = try TalosJSON.decode(GitOpsSourceFind.self, from: #"""
        {"matches":[{"path":"a.yaml","line":4},{"path":"a.yaml","line":30,"documentIndex":2}]}
        """#)
        XCTAssertEqual(several.outcome, .choose([ManifestMatch(path: "a.yaml", line: 4), ManifestMatch(path: "a.yaml", line: 30, documentIndex: 2)]))
        XCTAssertNotEqual(several.matches[0].id, several.matches[1].id)

        let helm = try TalosJSON.decode(GitOpsSourceFind.self, from: #"{"matches":null,"searched":0,"note":"rendered by Helm; open the chart's templates"}"#)
        XCTAssertEqual(helm.outcome, .none(note: "rendered by Helm; open the chart's templates"))
        XCTAssertEqual(try TalosJSON.decode(GitOpsSourceFind.self, from: "{}").outcome, .none(note: ""))
    }

    func testFirstBrowsableSource() throws {
        // The chart comes first and cannot be opened; the values-only source is not a row.
        let chart = GitSource(repoURL: "https://charts.example.org", kind: .chart, chart: "podinfo")
        let values = GitSource(repoURL: "v", webURL: "https://github.com/example-org/values", tracked: "main", kind: .values, browsable: true)
        let git = GitSource(repoURL: "g", webURL: "https://github.com/example-org/platform", tracked: "main", browsable: true)
        let picked = try XCTUnwrap(GitOpsSource(sources: [chart, values, git]).firstBrowsable)
        XCTAssertEqual(picked.index, 2)
        XCTAssertEqual(picked.source, git)

        let flux = FluxRef(kind: "GitRepository", namespace: "flux-system", name: "flux-system")
        XCTAssertEqual(GitOpsSource(sources: [GitSource(repoURL: "r", fluxArtifact: true, fluxSource: flux)]).firstBrowsable?.index, 0)
        XCTAssertNil(GitOpsSource(sources: [chart, GitSource(repoURL: "local", browsable: true)]).firstBrowsable)
        XCTAssertNil(GitOpsSource(sources: []).firstBrowsable)
    }
}
