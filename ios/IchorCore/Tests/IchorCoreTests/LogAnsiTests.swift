import XCTest
@testable import IchorCore

/// The shared vectors of go/ichorgo/logparse_ansi_test.go (ansiSampleLines): the entry JSON Go's
/// parseLogLine writes for each, pasted as is.
final class LogAnsiTests: XCTestCase {
    private static let samples = [
        #"{"ts":0,"level":"","source":"","msg":"INFO[0042] request done    client_ip=10.0.0.7 status=200","fields":[],"raw":"INFO[0042] request done    client_ip=10.0.0.7 status=200","spans":[{"t":"INFO","fg":6,"bg":-1},{"t":"[0042] request done    ","fg":-1,"bg":-1},{"t":"client_ip","fg":6,"bg":-1},{"t":"=10.0.0.7 ","fg":-1,"bg":-1},{"t":"status","fg":6,"bg":-1},{"t":"=200","fg":-1,"bg":-1}]}"#,
        #"{"ts":0,"level":"","source":"","msg":"ERRO[0043] upstream timeout error=\"dial tcp: i/o timeout\"","fields":[],"raw":"ERRO[0043] upstream timeout error=\"dial tcp: i/o timeout\"","spans":[{"t":"ERRO","fg":1,"bg":-1,"b":true},{"t":"[0043] upstream timeout ","fg":-1,"bg":-1},{"t":"error","fg":1,"bg":-1},{"t":"=\"dial tcp: i/o timeout\"","fg":-1,"bg":-1}]}"#,
        ##"{"ts":0,"level":"","source":"","msg":"WARN queue depth 850","fields":[],"raw":"WARN queue depth 850","spans":[{"t":"WARN","fg":-1,"bg":-1,"fgRgb":"#ff8700"},{"t":" queue depth ","fg":-1,"bg":-1},{"t":"850","fg":-1,"bg":-1,"fgRgb":"#ff8800"}]}"##,
        #"{"ts":0,"level":"","source":"","msg":"run 1 finished","fields":[],"raw":"run 1 finished","spans":[]}"#,
    ]

    private func entry(_ index: Int) -> LogEntry {
        parseLogEntry(json: Self.samples[index], line: "")
    }

    func testLogrusInfo() {
        let e = entry(0)
        XCTAssertEqual(e.raw, "INFO[0042] request done    client_ip=10.0.0.7 status=200")
        XCTAssertTrue(e.isColouredPlainLine)
        XCTAssertEqual(
            ansiRuns(e.spans),
            [
                AnsiRun("INFO", fg: .index(6)), AnsiRun("[0042] request done    "),
                AnsiRun("client_ip", fg: .index(6)), AnsiRun("=10.0.0.7 "),
                AnsiRun("status", fg: .index(6)), AnsiRun("=200"),
            ])
        XCTAssertEqual(ansiRuns(e.spans).map(\.text).joined(), e.raw)
    }

    func testLogrusError() {
        XCTAssertEqual(
            ansiRuns(entry(1).spans),
            [
                AnsiRun("ERRO", fg: .index(1), bold: true), AnsiRun("[0043] upstream timeout "),
                AnsiRun("error", fg: .index(1)), AnsiRun(#"="dial tcp: i/o timeout""#),
            ])
    }

    func testExactColours() {
        XCTAssertEqual(
            ansiRuns(entry(2).spans),
            [
                AnsiRun("WARN", fg: .rgb(r: 0xFF, g: 0x87, b: 0)), AnsiRun(" queue depth "),
                AnsiRun("850", fg: .rgb(r: 0xFF, g: 0x88, b: 0)),
            ])
    }

    func testLinkHasNoSpans() {
        let e = entry(3)
        XCTAssertEqual(e.raw, "run 1 finished")
        XCTAssertEqual(e.spans, [])
        XCTAssertFalse(e.isColouredPlainLine)
        XCTAssertEqual(ansiRuns(e.spans), [])
    }

    func testSpanDecodingIsLenient() throws {
        let span = try TalosJSON.decode(LogSpan.self, from: #"{"t":"x"}"#)
        XCTAssertEqual(span, LogSpan(t: "x"))
        XCTAssertEqual(try TalosJSON.decode(LogEntry.self, from: #"{"raw":"x","spans":null}"#).spans, [])
        XCTAssertEqual(try TalosJSON.decode(LogEntry.self, from: #"{"raw":"x"}"#).spans, [])
        let styled = try TalosJSON.decode(
            LogSpan.self, from: #"{"t":"x","fg":-1,"bg":4,"bgRgb":"","b":true,"d":true,"i":true,"u":true}"#)
        XCTAssertEqual(
            ansiRuns([styled]), [AnsiRun("x", bg: .index(4), bold: true, dim: true, italic: true, underline: true)])
    }

    func testColourMapping() {
        XCTAssertEqual(AnsiColor(index: -1, hex: ""), AnsiColor.none)
        XCTAssertEqual(AnsiColor(index: 15, hex: ""), .index(15))
        XCTAssertEqual(AnsiColor(index: 16, hex: ""), AnsiColor.none)
        XCTAssertEqual(AnsiColor(index: -1, hex: "#080808"), .rgb(r: 8, g: 8, b: 8))
        XCTAssertEqual(AnsiColor(index: 3, hex: "#zz0000"), .index(3))
        XCTAssertEqual(AnsiColor(index: -1, hex: "ff0000"), AnsiColor.none)
        XCTAssertTrue(AnsiRun("x").isPlain)
        XCTAssertFalse(AnsiRun("x", dim: true).isPlain)
        XCTAssertEqual(ansiRuns([LogSpan(t: "")]), [])
    }

    func testWorkloadLineSpans() throws {
        let line = try TalosJSON.decode(
            WorkloadLogLine.self, from: #"{"pod":"a","text":"INFO x","spans":[{"t":"INFO","fg":6,"bg":-1}]}"#)
        XCTAssertEqual(line.spans, [LogSpan(t: "INFO", fg: 6)])
        XCTAssertEqual(try TalosJSON.decode(WorkloadLogLine.self, from: #"{"pod":"a"}"#).spans, [])
    }

    func testLuminanceAndContrast() {
        XCTAssertEqual(relativeLuminance(r: 255, g: 255, b: 255), 1, accuracy: 1e-9)
        XCTAssertEqual(relativeLuminance(r: 0, g: 0, b: 0), 0, accuracy: 1e-9)
        XCTAssertEqual(contrastRatio(1, 0), 21, accuracy: 1e-9)
        XCTAssertEqual(contrastRatio(0, 1), 21, accuracy: 1e-9)
    }

    func testReadableNudge() {
        // Orange is under 3:1 on white: darkened, its hue kept (red ≥ green > blue).
        let orange = AnsiColor.rgb(r: 0xFF, g: 0x87, b: 0)
        let onLight = orange.readable(darkSurface: false)
        XCTAssertNotEqual(onLight, orange)
        guard case .rgb(let r, let g, let b) = onLight else { return XCTFail("\(onLight)") }
        XCTAssertGreaterThanOrEqual(
            contrastRatio(relativeLuminance(r: r, g: g, b: b), logLightSurfaceLuminance), logMinimumContrast)
        XCTAssertTrue(r > g && g > b)
        // Readable on the dark surface already.
        XCTAssertEqual(orange.readable(darkSurface: true), orange)
        // Dark blue on a dark surface is lightened; white on white goes to black at worst.
        let navy = AnsiColor.rgb(r: 0, g: 0, b: 0x40)
        guard case .rgb(let nr, let ng, let nb) = navy.readable(darkSurface: true) else { return XCTFail() }
        XCTAssertGreaterThanOrEqual(
            contrastRatio(relativeLuminance(r: nr, g: ng, b: nb), logDarkSurfaceLuminance), logMinimumContrast)
        let white = AnsiColor.rgb(r: 255, g: 255, b: 255)
        XCTAssertNotEqual(white.readable(darkSurface: false), white)
        // Indexed colours are the theme's business.
        XCTAssertEqual(AnsiColor.index(3).readable(darkSurface: false), .index(3))
        XCTAssertEqual(AnsiColor.none.readable(darkSurface: true), AnsiColor.none)
    }
}
