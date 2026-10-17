import XCTest
@testable import IchorCore

final class PullProblemTests: XCTestCase {
    private func pod(_ fields: String) throws -> KubePod {
        let json = #"{"pods":[{"namespace":"shop","name":"web-1","status":"ImagePullBackOff""# + fields + "}]}"
        return try XCTUnwrap(TalosJSON.decode(KubePodPage.self, from: json).pods.first)
    }

    func testDecodesWaitingAndPull() throws {
        let p = try pod(#","waiting":"429 Too Many Requests","pull":{"kind":"rateLimited","image":"nginx:1.27","registry":"docker.io","secrets":["regcred"]}"#)
        XCTAssertEqual(p.waiting, "429 Too Many Requests")
        XCTAssertEqual(p.pull, PullProblem(kind: .rateLimited, image: "nginx:1.27", registry: "docker.io", secrets: ["regcred"]))
    }

    func testDecodesWithoutWaitingNorPull() throws {
        let p = try pod("")
        XCTAssertEqual(p.waiting, "")
        XCTAssertNil(p.pull)
        let bare = try pod(#","pull":{}"#)
        XCTAssertEqual(bare.pull, PullProblem(kind: .unknown))
    }

    func testDecodesEveryKind() throws {
        for kind in PullKind.allCases {
            let p = try pod(#","pull":{"kind":""# + kind.rawValue + #""}"#)
            XCTAssertEqual(p.pull?.kind, kind)
        }
    }

    func testUnknownKindFallsBack() throws {
        XCTAssertEqual(try pod(#","pull":{"kind":"quotaExceeded"}"#).pull?.kind, .unknown)
        XCTAssertEqual(checkupPullKind(reason: ""), .unknown)
        XCTAssertEqual(checkupPullKind(reason: "Something else"), .unknown)
        XCTAssertEqual(checkupPullKind(reason: "tls"), .tls)
    }

    func testTitles() {
        let reg = "registry.example.test"
        XCTAssertEqual(PullProblem(kind: .notFound, registry: reg).title, .notFound(registry: reg))
        XCTAssertEqual(PullProblem(kind: .unauthorized, registry: reg).title, .unauthorized(registry: reg))
        XCTAssertEqual(PullProblem(kind: .rateLimited, registry: reg).title, .rateLimited(registry: reg))
        XCTAssertEqual(PullProblem(kind: .tls, registry: reg).title, .tls(registry: reg))
        XCTAssertEqual(PullProblem(kind: .network, registry: reg).title, .network(registry: reg))
        XCTAssertEqual(PullProblem(kind: .platform, registry: reg).title, .platform)
        XCTAssertEqual(PullProblem(kind: .invalidName, registry: reg).title, .invalidName)
        XCTAssertEqual(PullProblem(kind: .unknown, registry: reg).title, .unknown(registry: reg))
        // No host from the core: the image names the registry.
        XCTAssertEqual(PullProblem(kind: .notFound, image: "app:1").title, .notFound(registry: "app:1"))
    }

    func testHints() {
        XCTAssertEqual(PullProblem(kind: .unauthorized, secrets: ["a", "b"]).hint, .secrets("a, b"))
        XCTAssertEqual(PullProblem(kind: .unauthorized).hint, .noSecret)
        XCTAssertEqual(PullProblem(kind: .notFound).hint, .notFound)
        XCTAssertEqual(PullProblem(kind: .rateLimited).hint, .rateLimited)
        XCTAssertEqual(PullProblem(kind: .tls).hint, .tls)
        XCTAssertEqual(PullProblem(kind: .network).hint, .network)
        XCTAssertEqual(PullProblem(kind: .platform).hint, .platform)
        XCTAssertEqual(PullProblem(kind: .invalidName).hint, .invalidName)
        XCTAssertEqual(PullProblem(kind: .unknown).hint, .unknown)
    }

    func testActions() {
        XCTAssertEqual(PullProblem(kind: .rateLimited, registry: "docker.io").action, .registryMirror(host: "docker.io"))
        XCTAssertEqual(PullProblem(kind: .network).action, .netTools)
        for kind in PullKind.allCases where kind != .rateLimited && kind != .network {
            XCTAssertNil(PullProblem(kind: kind).action)
        }
    }
}
