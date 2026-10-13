import XCTest
@testable import IchorCore

final class ClusterGroupsTests: XCTestCase {
    private func kube(_ name: String, auth: String? = nil, demo: Bool = false) -> ContextSummary {
        ContextSummary(name: name, kind: ContextKind.kube, demo: demo, auth: auth)
    }

    private func omni(_ name: String, identity: String, authKey: String) -> ContextSummary {
        ContextSummary(name: name, signIn: "omni", omni: true, identity: identity, authKey: authKey)
    }

    private lazy var alpha = kube("arn:aws:eks:eu-west-1:111122223333:cluster/alpha", auth: "eks")
    private lazy var beta = kube("arn:aws:eks:us-east-1:444455556666:cluster/beta", auth: "eks")
    private lazy var gamma = kube("gke_sample-project_europe-west1_gamma", auth: "gke")
    private let home = ContextSummary(name: "admin@home")
    private let lab = ContextSummary(name: "admin@lab")
    private lazy var plain = kube("dev")
    private lazy var six = [home, alpha, gamma, lab, beta, plain]

    func testAShortListIsNotGrouped() {
        XCTAssertNil(clusterGroups(Array(six.prefix(5)), masked: false))
    }

    func testGroupsFollowTheFirstClusterOfEach() throws {
        let groups = try XCTUnwrap(clusterGroups(six, masked: false))
        XCTAssertEqual(groups.map(\.key), ["talos", "aws:111122223333", "gcp:sample-project", "aws:444455556666", "kubernetes"])
        XCTAssertEqual(groups.map(\.owner), [nil, "11…33", "sample-project", "44…66", nil])
        XCTAssertEqual(groups.first?.contexts, [home, lab])
        XCTAssertEqual(groups.map(\.logo), ["talos", "aws", "google-cloud", "aws", "kubernetes"])
    }

    func testScreenshotModeMergesTheAccounts() throws {
        let groups = try XCTUnwrap(clusterGroups(six, masked: true))
        XCTAssertEqual(groups.map(\.key), ["talos", "aws", "gcp", "kubernetes"])
        XCTAssertEqual(groups[1].contexts, [alpha, beta])
        XCTAssertEqual(groups.map(\.owner), [nil, nil, nil, nil])
    }

    func testOmniGroupsBySignIn() throws {
        let one = omni("omni-one", identity: "ops@example.com", authKey: "key-a")
        let two = omni("omni-two", identity: "ops@example.com", authKey: "key-a")
        let other = omni("omni-three", identity: "dev@example.com", authKey: "key-b")
        let groups = try XCTUnwrap(clusterGroups([one, two, other, home, lab, plain], masked: false))
        XCTAssertEqual(groups.map(\.key), ["omni:key-a", "omni:key-b", "talos", "kubernetes"])
        XCTAssertEqual(groups.first?.contexts, [one, two])
        XCTAssertEqual(groups.first?.owner, "ops@example.com")
        XCTAssertEqual(groups.first?.provider, .omni)
        XCTAssertEqual(clusterGroupKey(one, masked: true).key, "omni")
    }

    func testOtherCloudsGroupByProviderOnly() {
        XCTAssertEqual(clusterGroupKey(kube("do-sample", auth: "digitalocean"), masked: false),
                       ClusterGroup(key: "digitalocean", provider: .digitalOcean))
        XCTAssertEqual(clusterGroupKey(kube("aks-sample", auth: "azure"), masked: false).provider, .azure)
        XCTAssertEqual(clusterGroupKey(kube("rancher-sample", auth: "rancher"), masked: false).provider, .rancher)
        XCTAssertEqual(clusterGroupKey(kube("eks-sample", auth: "eks"), masked: false), ClusterGroup(key: "aws", provider: .aws))
    }

    func testDemoClustersGoByKind() {
        XCTAssertEqual(clusterGroupKey(ContextSummary(name: "demo", demo: true), masked: false).key, "talos")
        XCTAssertEqual(clusterGroupKey(kube("demo-kube", demo: true), masked: false).key, "kubernetes")
    }
}
