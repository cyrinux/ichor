import XCTest
@testable import IchorCore

final class PromTests: XCTestCase {
    func testDecodesTheGoJSON() throws {
        let json = #"{"resultType":"matrix","times":[1000,2000,3000],"series":[{"name":"up{job=\"a\"}","labels":{"job":"a"},"values":[1.5,null,2]}],"warnings":null,"truncated":false,"total":1}"#
        let res = try TalosJSON.decode(PromResult.self, from: json)
        XCTAssertEqual(res.times, [1000, 2000, 3000])
        XCTAssertEqual(res.series[0].values, [1.5, nil, 2])
        XCTAssertEqual(res.warnings, [])
        XCTAssertEqual(res.series[0].latest, 2)
        XCTAssertEqual(try TalosJSON.decode(PromResult.self, from: "{}"), PromResult())
        let found = try TalosJSON.decode(PromDiscovery.self, from: #"{"sources":[{"mode":"proxy","kind":"prometheus","namespace":"monitoring","service":"prometheus-operated","port":9090}]}"#)
        XCTAssertEqual(found.sources.first?.label, "monitoring/prometheus-operated:9090")
    }

    func testRefusedQueryIsRecognised() {
        XCTAssertTrue(isPromRefused("https://m.example/prometheus: HTTP 401, refused (credentials or tenant): no org id"))
        XCTAssertFalse(isPromRefused("https://m.example/prometheus: HTTP 404 page not found: check the path prefix"))
        XCTAssertFalse(isPromRefused(nil))
    }

    func testLegendFillsLabels() {
        let s = PromSeries(name: "x{a=\"1\"}", labels: ["namespace": "kube-system", "pod": "coredns"])
        XCTAssertEqual(s.legend("{{namespace}}/{{ pod }}"), "kube-system/coredns")
        XCTAssertEqual(s.legend(""), "x{a=\"1\"}")
        XCTAssertEqual(s.legend("{{missing}}"), "x{a=\"1\"}")
        XCTAssertEqual(s.legend("pod {{pod"), "pod {{pod")
    }

    func testSourceNeverPrintsItsSecretAndDropsOtherModeFields() throws {
        let url = PromSource(mode: PromSource.url, url: "https://m.example/prometheus", auth: PromSource.authBearer, secret: "s3cret")
        XCTAssertFalse(url.description.contains("s3cret"))
        let proxy = url.switched(to: PromSource.proxy)
        XCTAssertEqual(proxy.secret, "")
        XCTAssertEqual(proxy.auth, PromSource.authNone)
        // Round trip through the Go field names.
        let data = try JSONEncoder().encode(url)
        XCTAssertEqual(try JSONDecoder().decode(PromSource.self, from: data), url)
    }

    func testPanelsSaveAndMove() {
        let a = PromPanel(id: "a", title: "A", query: "up")
        let b = PromPanel(id: "b", title: "B", query: "up")
        var config = MetricsConfig(panels: [a, b])
        config = config.moving("b", by: -1)
        XCTAssertEqual(config.panels.map(\.id), ["b", "a"])
        XCTAssertEqual(config.moving("b", by: -1), config)
        config = config.saving(PromPanel(id: "a", title: "A2", query: "up"))
        XCTAssertEqual(config.panels.map(\.title), ["B", "A2"])
        XCTAssertEqual(config.saving(PromPanel(id: "c")).panels.count, 3)
    }

    func testValuesFormatByUnit() {
        XCTAssertEqual(formatMetric(42.123, unit: "percent"), "42.1%")
        XCTAssertEqual(formatMetric(1.5 * 1024 * 1024 * 1024, unit: "bytes"), "1.5 GiB")
        XCTAssertEqual(formatMetric(0.25, unit: "cores"), "0.25")
        XCTAssertEqual(formatMetric(100, unit: "persec"), "100/s")
        XCTAssertEqual(formatMetric(7, unit: "count"), "7")
        XCTAssertEqual(compactMetric(1_234_567), "1.23M")
        XCTAssertEqual(compactMetric(12.5), "12.5")
        XCTAssertEqual(compactMetric(1), "1")
        XCTAssertEqual(compactMetric(999.6), "1000")
        XCTAssertEqual(compactMetric(0.000123), "1.23e-04")
        XCTAssertEqual(compactMetric(-12.5), "-12.5")
    }
}
