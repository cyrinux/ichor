import XCTest
@testable import IchorCore

final class ImagePullTests: XCTestCase {
    func testProgressDecoding() throws {
        let p = try TalosJSON.decode(ImagePullProgress.self, from: """
        {"nodes":[{"node":"10.0.0.2","hostname":"cp-1","state":"done"},
         {"node":"10.0.0.3","hostname":"","state":"failed","error":"not found"},
         {"node":"10.0.0.4","hostname":"w-2","state":"queued"}],"done":1,"total":3,"at":5}
        """)
        XCTAssertEqual(p.done, 1)
        XCTAssertEqual(p.total, 3)
        XCTAssertEqual(p.failed, 1)
        XCTAssertEqual(p.nodes.map(\.state), [.done, .failed, .pending])
        XCTAssertEqual(p.nodes[1].label, "10.0.0.3")
        XCTAssertEqual(p.nodes[1].error, "not found")
        XCTAssertEqual(try TalosJSON.decode(ImagePullProgress.self, from: #"{"nodes":null}"#).nodes, [])
    }

    func testNamespacesMatchTheGoNames() {
        XCTAssertEqual(ImagePullNamespace.allCases.map(\.rawValue), ["system", "cri"])
    }

    func testImagePullIsANodeFeature() {
        XCTAssertEqual(NodeFeature(rawValue: "imagePull"), .imagePull)
    }
}
