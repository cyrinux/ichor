import XCTest
@testable import IchorCore

final class ImageScanTests: XCTestCase {
    // As go/ichorgo/imagescan_report.go writes it.
    private let json = """
        {"source":"scan","scanner":"Trivy 0.75.0","started":1,"finished":2,"images":[
         {"image":"nginx:1.25","ref":"docker.io/library/nginx@sha256:abc","digest":"sha256:abc","os":"debian 12.4",
          "pods":["web/a"],"scannedAt":2,"summary":{"critical":1,"high":1,"medium":0,"low":1,"unknown":0,"fixable":2,"os":3},
          "vulnerabilities":[
           {"id":"CVE-1","package":"libssl3","installed":"3.0.11","fixed":"3.0.13","severity":"CRITICAL","score":9.8,"class":"os-pkgs","target":"nginx (debian 12.4)"},
           {"id":"CVE-2","package":"libssl3","installed":"3.0.11","fixed":"3.0.13","severity":"HIGH","class":"os-pkgs"},
           {"id":"CVE-3","package":"perl-base","installed":"5.36","severity":"LOW","class":"os-pkgs"}]},
         {"image":"ghcr.io/x/app:2","ref":"ghcr.io/x/app:2","pods":["web/a"],"error":"the image is no longer in its registry",
          "summary":{"critical":0,"high":0,"medium":0,"low":0,"unknown":0,"fixable":0,"os":0},"vulnerabilities":null}]}
        """

    private func report() throws -> ImageScanReport { try TalosJSON.decode(ImageScanReport.self, from: json) }

    func testDecodesTheGoJSON() throws {
        let report = try report()
        let nginx = report.images[0]
        XCTAssertEqual(nginx.os, "debian 12.4")
        XCTAssertEqual(nginx.vulnerabilities[0].package, "libssl3")
        XCTAssertEqual(nginx.vulnerabilities[0].class, "os-pkgs")
        XCTAssertEqual(nginx.vulnerabilities[0].level, .critical)
        XCTAssertEqual(report.images[1].vulnerabilities, [])
        XCTAssertEqual(report.failed, 1)
        XCTAssertEqual(report.summary, VulnSummary(critical: 1, high: 1, low: 1, fixable: 2, os: 3))
        XCTAssertEqual(report.summary.total, 3)

        let operatorReports = try TalosJSON.decode(OperatorReports.self, from: #"{"available":true,"report":\#(json)}"#)
        XCTAssertTrue(operatorReports.available)
        XCTAssertEqual(operatorReports.report.images.count, 2)
    }

    func testReEncodesForTheExports() throws {
        // An operator report goes back to the core as JSON: the same field names.
        let report = try report()
        let encoded = try JSONEncoder().encode(report)
        let again = try JSONDecoder().decode(ImageScanReport.self, from: encoded)
        XCTAssertEqual(again, report)
        XCTAssertTrue(String(decoding: encoded, as: UTF8.self).contains(#""class":"os-pkgs""#))
    }

    func testFiltersAndGroupsByPackage() throws {
        let nginx = try report().images[0]

        // Fixable only (the default): perl-base has no fix.
        let fixable = nginx.byPackage(VulnFilter())
        XCTAssertEqual(fixable.map(\.package), ["libssl3 3.0.11"])
        XCTAssertEqual(fixable[0].vulns.map(\.id), ["CVE-1", "CVE-2"])

        XCTAssertEqual(nginx.byPackage(VulnFilter(fixableOnly: false)).map(\.package), ["libssl3 3.0.11", "perl-base 5.36"])

        var noHigh = VulnFilter(fixableOnly: false)
        noHigh.toggle(.high)
        XCTAssertFalse(noHigh.severities.contains(.high))
        XCTAssertEqual(nginx.byPackage(noHigh).flatMap { $0.vulns.map(\.id) }, ["CVE-1", "CVE-3"])
        noHigh.toggle(.high)
        XCTAssertTrue(noHigh.severities.contains(.high))
    }

    func testOnlyAScanThatScannedSomethingReplacesTheOperatorReport() throws {
        let report = try report()
        var done = ImageScanState(context: "ctx", appID: "nginx")
        done.finish(report: report, json: json, error: nil)
        XCTAssertEqual(done.usableReport, report)
        XCTAssertFalse(done.running)

        var stopped = ImageScanState(context: "ctx", appID: "nginx")
        stopped.stopping = true
        stopped.finish(report: report, json: json, error: "image scan stopped")
        XCTAssertTrue(stopped.stopped)
        XCTAssertNil(stopped.error)
        XCTAssertNil(stopped.usableReport)

        var failed = ImageScanState(context: "ctx", appID: "nginx")
        failed.finish(report: report, json: json, error: imageScanTimedOut)
        XCTAssertNil(failed.usableReport)

        // Stopped before the first image: every image "not scanned".
        let nothing = ImageScanReport(source: "scan", images: report.images.map { ScannedImage(image: $0.image, error: imageScanNotScanned) })
        var empty = ImageScanState(context: "ctx", appID: "nginx")
        empty.finish(report: nothing, json: "", error: nil)
        XCTAssertNil(empty.usableReport)
    }

    func testSeveritiesAndSummaries() {
        XCTAssertEqual(VulnSeverity(wire: "bogus"), .unknown)
        XCTAssertEqual(VulnSeverity(wire: "MEDIUM"), .medium)
        XCTAssertEqual(VulnSummary(high: 1) + VulnSummary(high: 1, fixable: 1), VulnSummary(high: 2, fixable: 1))
        XCTAssertEqual(ImageScanFormat.allCases.map(\.rawValue), ["html", "sarif", "cyclonedx", "csv", "json"])
    }

    func testMadeAtAndFilename() throws {
        let operatorReport = ImageScanReport(source: "operator", finished: 5, images: [ScannedImage(image: "a", scannedAt: 3), ScannedImage(image: "b", scannedAt: 4)])
        XCTAssertEqual(operatorReport.madeAt, 4)
        XCTAssertEqual(ImageScanReport(source: "operator", finished: 5, images: [ScannedImage(image: "a")]).madeAt, 5)
        XCTAssertEqual(try report().madeAt, 2)

        let date = Date(timeIntervalSince1970: 0)
        let name = imageScanFilename(app: "Home Assistant / Core!", date: date, format: .cyclonedx)
        XCTAssertTrue(name.hasPrefix("home-assistant-core-vulnerabilities-"), name)
        XCTAssertTrue(name.hasSuffix(".cdx.json"), name)
        XCTAssertEqual(imageScanFilename(app: "***", date: date, format: .csv).prefix(20), "app-vulnerabilities-")
    }
}
