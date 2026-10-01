import XCTest
@testable import TalosdevMobileCore

final class ImagesTests: XCTestCase {
    private let images = [
        ContainerImage(name: "registry.k8s.io/pause:3.10", digest: "sha256:aaa", size: 300_000, created: 0),
        ContainerImage(name: "ghcr.io/siderolabs/flannel:v0.26", digest: "sha256:bbb", size: 30_000_000, created: 2_000),
        ContainerImage(name: "docker.io/library/nginx:1.27", digest: "sha256:ccc", size: 70_000_000, created: 1_000),
    ]

    func testDecodeGoJSON() throws {
        let list = try TalosJSON.decode([ContainerImage].self, from: #"[{"name":"a:1","digest":"sha256:x","size":10,"created":5}]"#)
        XCTAssertEqual(list, [ContainerImage(name: "a:1", digest: "sha256:x", size: 10, created: 5)])
    }

    func testSort() {
        XCTAssertEqual(sortImages(images, by: .name).map(\.digest), ["sha256:ccc", "sha256:bbb", "sha256:aaa"])
        XCTAssertEqual(sortImages(images, by: .size).map(\.digest), ["sha256:ccc", "sha256:bbb", "sha256:aaa"])
        XCTAssertEqual(sortImages(images, by: .created).map(\.digest), ["sha256:bbb", "sha256:ccc", "sha256:aaa"])
    }

    func testFilterAndTotal() {
        XCTAssertEqual(filterImages(images, query: "FLANNEL").map(\.digest), ["sha256:bbb"])
        XCTAssertEqual(filterImages(images, query: "sha256:a").map(\.digest), ["sha256:aaa"])
        XCTAssertEqual(filterImages(images, query: " ").count, 3)
        XCTAssertEqual(totalImageSize(images), 100_300_000)
    }
}
