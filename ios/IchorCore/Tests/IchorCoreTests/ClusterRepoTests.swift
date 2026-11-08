import XCTest
@testable import IchorCore

final class ClusterRepoTests: XCTestCase {
    func testNormalizeTrimsAndCleansDirectories() {
        let repo = normalizeClusterRepo(
            ClusterRepo(
                repoURL: "  git@github.com:example-org/cluster-config.git ", branch: " main ",
                talosDir: "/talos/", kubernetesDir: " kubernetes/ "))
        XCTAssertEqual(
            repo,
            ClusterRepo(
                repoURL: "git@github.com:example-org/cluster-config.git", branch: "main",
                talosDir: "talos", kubernetesDir: "kubernetes"))
        XCTAssertEqual(repo?.host, "github.com")
        XCTAssertNil(normalizeClusterRepo(ClusterRepo(repoURL: "  ")))
    }

    func testWebURLOfEveryAddressForm() {
        XCTAssertEqual(
            repoWebURL("git@github.com:example-org/cluster-config.git"), "https://github.com/example-org/cluster-config"
        )
        XCTAssertEqual(
            repoWebURL("ssh://git@gitlab.example.org/group/sub/infra.git"), "https://gitlab.example.org/group/sub/infra"
        )
        XCTAssertEqual(
            repoWebURL("https://git.home.example/example-org/cluster-config/"),
            "https://git.home.example/example-org/cluster-config")
        XCTAssertEqual(repoWebURL("not a repo"), "")
    }

    func testRepoSourceIsTheRootAtTheHead() throws {
        let json =
            #"{"defaultBranch":"main","branch":"main","headSha":"0123456789abcdef0123456789abcdef01234567","webURL":"https://github.com/example-org/cluster-config","private":true,"canPush":true,"forge":"github"}"#
        let info = try JSONDecoder().decode(GitRepoInfo.self, from: Data(json.utf8))
        XCTAssertTrue(info.isPrivate)
        XCTAssertEqual(info.crumb, "main @ 0123456")
        let source = repoSource(ClusterRepo(repoURL: "git@github.com:example-org/cluster-config.git"), info: info)
        XCTAssertEqual(source.path, "")
        XCTAssertEqual(source.tracked, "main")
        XCTAssertEqual(source.ref, info.headSha)
        XCTAssertEqual(source.read, .forge(host: "github.com", ref: info.headSha))
        XCTAssertEqual(source.proposalHost, "github.com")
        XCTAssertEqual(proposalBaseRef(for: source), "main")
        XCTAssertTrue(GitOpsAppRef.repository.isRepository)
        XCTAssertFalse(GitOpsAppRef.argo(namespace: "argocd", name: "web").isRepository)
    }

    func testRepoSourceWithoutAWebPageBuildsOne() {
        let source = repoSource(
            ClusterRepo(repoURL: "git@gitea.home.example:ops/fleet.git", branch: "dev"),
            info: GitRepoInfo(defaultBranch: "main"))
        XCTAssertEqual(source.webURL, "https://gitea.home.example/ops/fleet")
        XCTAssertEqual(source.tracked, "dev")
    }

    func testStoreRoundTripsAndForgetsRemovedClusters() {
        let repos = [
            "fp-lab": ClusterRepo(repoURL: "https://github.com/example-org/cluster-config", talosDir: "talos"),
            "fp-gone": ClusterRepo(repoURL: "https://github.com/example-org/old"),
        ]
        XCTAssertEqual(ClusterRepoStore.decode(ClusterRepoStore.encode(repos)), repos)
        XCTAssertNil(ClusterRepoStore.encode([:]))
        XCTAssertEqual(ClusterRepoStore.decode(nil), [:])
        XCTAssertEqual(ClusterRepoStore.keep(repos, fingerprints: ["fp-lab", ""]).keys.sorted(), ["fp-lab"])
    }

    func testBackupRoundTrip() throws {
        let repo = ClusterRepo(
            repoURL: "https://github.com/example-org/cluster-config", branch: "main", talosDir: "talos",
            kubernetesDir: "kubernetes")
        let clusters = backupClusters(
            fingerprints: ["fp-lab", "fp-other"], names: [:], colors: [:], kubeServers: [:],
            repos: ["fp-lab": repo])
        XCTAssertEqual(clusters["fp-lab"]?.repository, repo)
        XCTAssertNil(clusters["fp-other"]?.repository)
        let decoded = try JSONDecoder().decode([String: BackupCluster].self, from: JSONEncoder().encode(clusters))
        let restored = restoredClusters(decoded, fingerprints: ["fp-lab"])
        XCTAssertEqual(restored.repos, ["fp-lab": repo])
        // An entry without an address is dropped, an unknown cluster's too.
        let blank = [
            "fp-lab": BackupCluster(repository: ClusterRepo(repoURL: " ")), "fp-new": BackupCluster(repository: repo),
        ]
        XCTAssertEqual(restoredClusters(blank, fingerprints: ["fp-lab"]).repos, [:])
    }

    func testAndroidBackupShapeDecodes() throws {
        // Android leaves blank fields out (no encodeDefaults).
        let json =
            #"{"fp-lab":{"repository":{"repoURL":"https://github.com/example-org/cluster-config","talosDir":"talos"}}}"#
        let clusters = try JSONDecoder().decode([String: BackupCluster].self, from: Data(json.utf8))
        XCTAssertEqual(restoredClusters(clusters, fingerprints: ["fp-lab"]).repos["fp-lab"]?.talosDir, "talos")
    }
}
