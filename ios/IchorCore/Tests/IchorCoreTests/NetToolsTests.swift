import XCTest
@testable import IchorCore

final class NetToolsTests: XCTestCase {
    func testResultsDecode() throws {
        let dns = try TalosJSON.decode(NetToolResult.self, from: """
        {"tool":"dns","target":"kubernetes.default.svc.cluster.local","ok":true,"exitCode":0,
         "dns":{"status":"NOERROR","records":[{"name":"kubernetes.default.svc.cluster.local.","type":"A","ttl":30,"value":"10.96.0.1"}],
         "server":"10.96.0.10","queryMs":2},"raw":"..."}
        """)
        XCTAssertEqual(dns.dns?.records.first?.value, "10.96.0.1")
        XCTAssertNil(dns.ping)

        let trace = try TalosJSON.decode(NetToolResult.self, from: """
        {"tool":"trace","target":"203.0.113.5","ok":true,"trace":[{"hop":1,"host":"10.0.0.1","lossPct":0,"avgMs":0.3},
         {"hop":2,"host":"???","lossPct":100,"avgMs":0}],"raw":""}
        """)
        XCTAssertEqual(trace.trace?.map(\.silent), [false, true])

        let http = try TalosJSON.decode(NetToolResult.self, from: """
        {"tool":"http","target":"https://www.example.test","ok":true,
         "http":{"status":200,"totalMs":84.2,"tls":true,"tlsOk":true,"notAfter":"2026-11-30T23:59:59Z","daysLeft":51},"raw":""}
        """)
        XCTAssertEqual(http.http?.daysLeft, 51)
        XCTAssertEqual(http.http?.error, "")
    }

    func testTargets() {
        XCTAssertEqual(netTargetProblem(.dns, " "), .empty)
        XCTAssertEqual(netTargetProblem(.dns, "example.test; reboot"), .characters)
        XCTAssertEqual(netTargetProblem(.ping, "-f"), .characters)
        XCTAssertEqual(netTargetProblem(.port, "203.0.113.5"), .hostPort)
        XCTAssertEqual(netTargetProblem(.http, "example.test"), .url)
        XCTAssertEqual(netTargetProblem(.trace, "https://example.test"), .host)
        XCTAssertNil(netTargetProblem(.port, "[2001:db8::1]:6443"))
        XCTAssertNil(netTargetProblem(.http, "https://example.test/healthz?verbose"))
        XCTAssertEqual(rememberNetTarget(["a.example.test", "b.example.test"], " b.example.test "), ["b.example.test", "a.example.test"])
        XCTAssertEqual(rememberNetTarget((1...12).map { "h\($0).example.test" }, "n.example.test").count, 10)
    }
}
