import XCTest

@testable import IchorCore

final class KubeBastionTests: XCTestCase {
    func testProblemsFromCodes() {
        XCTAssertEqual(bastionProblem("kube-bastion-unreachable: dial tcp: timeout"), .unreachable)
        XCTAssertEqual(bastionProblem("list pods: kube-bastion-host-key: pinned key differs"), .hostKey)
        XCTAssertEqual(bastionProblem("kube-bastion-auth"), .auth)
        XCTAssertEqual(bastionProblem("kube-bastion-key: bad passphrase"), .key)
        XCTAssertEqual(bastionProblem("kube-bastion-forward: administratively prohibited"), .forward)
    }

    func testOtherFailuresAreNoBastionProblem() {
        XCTAssertNil(bastionProblem(nil))
        XCTAssertNil(bastionProblem("connection refused"))
        XCTAssertNil(bastionProblem("kube-bastion-unavailable: proxy-url"))
        XCTAssertNil(bastionProblem("kube-bastion-keys"))
        XCTAssertNil(bastionProblem("x-kube-bastion-auth"))
    }

    func testInfoDecodes() throws {
        let info = try KubeBastionInfo.decode(
            #"{"configured":true,"host":"bastion.example.net:22","user":"jump","auth":"password","#
                + #""hostKeyType":"ssh-ed25519","hostKeyFingerprint":"SHA256:abc"}"#)
        XCTAssertTrue(info.configured)
        XCTAssertEqual(info.displayHost, "bastion.example.net")
        XCTAssertFalse(info.usesKey)
        XCTAssertEqual(info.unavailable, "")
        XCTAssertTrue(try KubeBastionInfo.decode("{}").usesKey)
    }

    func testDisplayHostKeepsOtherPorts() {
        XCTAssertEqual(KubeBastionInfo(host: "bastion.example.net:2222").displayHost, "bastion.example.net:2222")
    }

    func testFieldsSendOnlyWhatIsGiven() throws {
        let fields = BastionFields(host: " bastion.example.net ", user: "jump ", password: " pw ")
        XCTAssertEqual(fields.values, ["host": "bastion.example.net", "user": "jump", "password": " pw "])
        XCTAssertEqual(BastionFields(hostKey: "ssh-ed25519 AAAA").values, ["hostKey": "ssh-ed25519 AAAA"])
        XCTAssertEqual(try BastionFields(user: "jump").json(), #"{"user":"jump"}"#)
    }

    func testOfferedOnlyForPlainKubeconfigClusters() {
        let kube = ContextSummary(name: "prod", kind: ContextKind.kube, fingerprint: "k1")
        XCTAssertTrue(bastionOffered(kube, masked: false))
        XCTAssertFalse(bastionOffered(kube, masked: true))
        XCTAssertFalse(bastionOffered(ContextSummary(name: "lab"), masked: false))
        XCTAssertFalse(
            bastionOffered(
                ContextSummary(name: "p", kind: ContextKind.kube, proxyURL: "socks5://proxy.example.net:1080"),
                masked: false))
        XCTAssertFalse(bastionOffered(ContextSummary(name: "demo", kind: ContextKind.kube, demo: true), masked: false))
    }
}
