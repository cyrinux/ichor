import XCTest
@testable import IchorCore

final class KubePortForwardTests: XCTestCase {
    func testContainerPortsFromGoYAML() {
        // go.yaml.in/yaml/v4 output: 4-space indent, sorted keys.
        let yaml = """
        apiVersion: v1
        kind: Pod
        metadata:
            name: web
        spec:
            containers:
                - image: nginx
                  name: web
                  ports:
                    - containerPort: 8080
                      name: http
                      protocol: TCP
                    - containerPort: 9090
                      name: metrics
                      protocol: TCP
                    - containerPort: 53
                      name: dns
                      protocol: UDP
                - image: sidecar
                  name: proxy
                  ports:
                    - containerPort: 8080
                      protocol: TCP
                    - containerPort: 15000
        status:
            phase: Running
        """
        XCTAssertEqual(containerPorts(fromPodYAML: yaml), [
            KubeContainerPort(port: 8080, name: "http"),
            KubeContainerPort(port: 9090, name: "metrics"),
            KubeContainerPort(port: 15000),
        ])
    }

    func testContainerPortsTwoSpaceIndentAndKeysBeforePort() {
        let yaml = """
        spec:
          containers:
          - name: app
            ports:
            - name: "web"
              protocol: TCP
              containerPort: 3000
            - containerPort: 0
            - containerPort: notaport
        """
        XCTAssertEqual(containerPorts(fromPodYAML: yaml), [KubeContainerPort(port: 3000, name: "web")])
        XCTAssertEqual(containerPorts(fromPodYAML: ""), [])
        XCTAssertEqual(containerPorts(fromPodYAML: "spec:\n  containers:\n  - name: a\n"), [])
    }

    func testPortForwardURL() {
        XCTAssertEqual(portForwardURL("127.0.0.1:54321")?.absoluteString, "http://127.0.0.1:54321")
        XCTAssertNil(portForwardURL("0.0.0.0:80"))
        XCTAssertNil(portForwardURL("127.0.0.1"))
        XCTAssertNil(portForwardURL("127.0.0.1:99999"))
        XCTAssertNil(portForwardURL(""))
    }

    func testParsePort() {
        XCTAssertEqual(parsePort(" 8080 "), 8080)
        XCTAssertNil(parsePort("0"))
        XCTAssertNil(parsePort("65536"))
        XCTAssertNil(parsePort("http"))
    }

    func testFollowedLinesKeepTheNewest() {
        let kept = appendCapped(Array(0..<4_990), Array(4_990..<5_020), cap: 5_000)
        XCTAssertEqual(kept.count, 5_000)
        XCTAssertEqual(kept.first, 20)
        XCTAssertEqual(kept.last, 5_019)
    }
}
