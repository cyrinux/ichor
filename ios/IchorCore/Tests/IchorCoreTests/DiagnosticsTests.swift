import XCTest
@testable import IchorCore

final class DiagnosticsTests: XCTestCase {
    // MARK: - MetricKit payloads (invented values, in jsonRepresentation()'s shape)

    /// A frame and its caller chain: `binary+offset` from the innermost frame down.
    private static func frameChain(_ count: Int) -> String {
        var json = ""
        for i in (0..<count).reversed() {
            let sub = json.isEmpty ? "" : #", "subFrames" : [\#(json)]"#
            json = #"""
                {"binaryUUID" : "00000000-0000-0000-0000-00000000000\#(i % 10)", "offsetIntoBinaryTextSegment" : \#(1000 + i), "sampleCount" : 1, "binaryName" : "\#(i == 0 ? "libsystem_kernel.dylib" : "Ichor")", "address" : \#(4_295_000_000 + i)\#(sub)}
                """#
        }
        return json
    }

    private static let crashPayload = #"""
        {
          "timeStampBegin" : "2026-10-28 09:00:00",
          "timeStampEnd" : "2026-10-28 09:59:00",
          "crashDiagnostics" : [
            {
              "version" : "1.0.0",
              "callStackTree" : {
                "callStackPerThread" : true,
                "callStacks" : [
                  {"threadAttributed" : false, "callStackRootFrames" : [{"binaryName" : "Other", "offsetIntoBinaryTextSegment" : 1, "sampleCount" : 1}]},
                  {"threadAttributed" : true, "callStackRootFrames" : [\#(frameChain(14))]}
                ]
              },
              "diagnosticMetaData" : {
                "appVersion" : "1.42.0", "appBuildVersion" : "142", "osVersion" : "iPhone OS 18.1 (22B83)",
                "deviceType" : "iPhone17,1", "platformArchitecture" : "arm64e", "bundleIdentifier" : "name.levis.ichor",
                "exceptionType" : 1, "exceptionCode" : 0, "signal" : 11,
                "terminationReason" : "Namespace SIGNAL, Code 0xb", "isTestFlightApp" : false, "pid" : 4321
              }
            }
          ]
        }
        """#

    private static let hangPayload = #"""
        {
          "timeStampBegin" : "2026-10-27 18:00:00",
          "timeStampEnd" : "2026-10-27 18:59:00",
          "hangDiagnostics" : [
            {
              "version" : "1.0.0",
              "callStackTree" : {"callStackPerThread" : false, "callStacks" : [{"callStackRootFrames" : [\#(frameChain(3))]}]},
              "diagnosticMetaData" : {"appVersion" : "1.42.0", "appBuildVersion" : "142", "hangDuration" : "8 sec"}
            }
          ],
          "diskWriteExceptionDiagnostics" : [
            {"diagnosticMetaData" : {"appVersion" : "1.42.0", "writesCaused" : "1,024 MB"}}
          ]
        }
        """#

    func testSummarizeACrash() throws {
        let records = CrashRecord.summarize(metricKitJSON: Self.crashPayload)
        let crash = try XCTUnwrap(records.first)
        XCTAssertEqual(records.count, 1)
        XCTAssertEqual(crash.kind, .crash)
        XCTAssertEqual(crash.begin, "2026-10-28 09:00:00")
        XCTAssertEqual(crash.end, "2026-10-28 09:59:00")
        XCTAssertEqual(crash.appVersion, "1.42.0 (142)")
        XCTAssertEqual(crash.terminationReason, "Namespace SIGNAL, Code 0xb")
        XCTAssertEqual(crash.signal, 11)
        XCTAssertEqual(crash.exceptionType, 1)
        XCTAssertEqual(crash.exceptionCode, 0)
        // The attributed thread, capped at 10 frames, innermost first.
        XCTAssertEqual(crash.frames.count, 10)
        XCTAssertEqual(crash.frames.first, "libsystem_kernel.dylib+1000")
        XCTAssertEqual(crash.frames[1], "Ichor+1001")
        XCTAssertEqual(crash.frames.last, "Ichor+1009")
        XCTAssertEqual(crash.codesLine, "signal 11 · exception 1 (code 0)")
    }

    func testSummarizeAHangAndAnException() {
        let records = CrashRecord.summarize(metricKitJSON: Self.hangPayload)
        XCTAssertEqual(records.map(\.kind), [.hang, .diskWriteException])
        XCTAssertEqual(records[0].duration, "8 sec")
        XCTAssertEqual(records[0].frames, ["libsystem_kernel.dylib+1000", "Ichor+1001", "Ichor+1002"])
        XCTAssertNil(records[0].signal)
        XCTAssertEqual(records[0].terminationReason, "")
        XCTAssertEqual(records[1].duration, "1,024 MB")
        XCTAssertEqual(records[1].appVersion, "1.42.0")
        XCTAssertTrue(records[1].frames.isEmpty)
    }

    func testSummarizeSomethingElse() {
        XCTAssertTrue(CrashRecord.summarize(metricKitJSON: "not json").isEmpty)
        XCTAssertTrue(CrashRecord.summarize(metricKitJSON: "[]").isEmpty)
        XCTAssertTrue(CrashRecord.summarize(metricKitJSON: #"{"timeStampBegin":"x"}"#).isEmpty)
    }

    func testRingAttrsHoldNumbersAndTheKindOnly() throws {
        let crash = try XCTUnwrap(CrashRecord.summarize(metricKitJSON: Self.crashPayload).first)
        XCTAssertEqual(
            DiagnosticsFiles.attrsJSON(crash.ringAttrs),
            #"{"exception_code":0,"exception_type":1,"frames":10,"kind":"crash","signal":11}"#)
    }

    // MARK: - The log (step 1's DiagEntries)

    /// `just probe diag-tail` against a closed local port, newest first (the Android test's sample).
    private static let entries = """
        [
          {"at":1793284805123,"level":"warn","component":"api","msg":"call failed","attrs":{"fn":"ClusterOverview"},"err":"dial tcp <redacted>: connection refused"},
          {"at":1793284805001,"level":"info","component":"trace","msg":"startup step","attrs":{"step":"config.parse","ms":12,"count":3}},
          {"at":1793284800000,"level":"info","component":"ios","msg":"startup app.init start","attrs":{"since_start_ms":41,"delta_ms":41,"ok":true}}
        ]
        """

    func testDecodesTheEntriesTheCoreWrites() throws {
        let decoded = try TalosJSON.decode([DiagEntry].self, from: Self.entries)
        XCTAssertEqual(decoded.count, 3)
        XCTAssertEqual(decoded[0].attrs["fn"], .text("ClusterOverview"))
        XCTAssertEqual(decoded[0].err, "dial tcp <redacted>: connection refused")
        XCTAssertTrue(decoded[0].isProblem)
        XCTAssertEqual(decoded[1].attrsLine, "count=3 ms=12 step=config.parse")
        XCTAssertEqual(decoded[2].attrs["ok"], .bool(true))
        XCTAssertEqual(decoded[2].err, "")
        XCTAssertEqual(decoded[2].date.timeIntervalSince1970, 1_793_284_800, accuracy: 0.001)
    }

    func testAnEntryWithoutItsMessageDoesNotDecode() {
        XCTAssertThrowsError(
            try TalosJSON.decode([DiagEntry].self, from: #"[{"at":1,"level":"info","component":"api"}]"#))
    }

    func testDecodesTheExportSummary() throws {
        let summary = try TalosJSON.decode(
            DiagExportSummary.self,
            from: #"""
                {"path":"/private/var/mobile/tmp/diagnostics/ichor-diagnostics-20261029-1412.zip","bytes":2048,"entries":3,"files":["README.txt","app.json","log.jsonl","platform.json","actions.md"]}
                """#)
        XCTAssertEqual(summary.files.count, 5)
        XCTAssertEqual(summary.entries, 3)
        XCTAssertEqual(summary.bytes, 2048)
    }

    // MARK: - app.json and the files

    func testAppInfoWritesEveryField() throws {
        let info = AppInfo(
            version: nil, build: nil, system: "iOS", systemVersion: "18.1", model: nil, locale: nil, dataStore: nil)
        let object = try XCTUnwrap(
            JSONSerialization.jsonObject(with: Data(info.json().utf8)) as? [String: Any])
        XCTAssertEqual(
            Set(object.keys), ["version", "build", "system", "systemVersion", "model", "locale", "dataStore"])
        XCTAssertEqual(object["version"] as? String, "")
        let store = try XCTUnwrap(object["dataStore"] as? [String: Any])
        XCTAssertEqual(Set(store.keys), ["persisting", "dir", "reason"])
    }

    func testAppInfoCarriesTheDeviceAndTheCoreStatus() throws {
        let info = AppInfo(
            version: "1.42.0", build: "142", system: "iOS", systemVersion: "18.1", model: "iPhone17,1",
            locale: "fr-FR", dataStore: DataStoreStatus(persisting: true, dir: "core"))
        let object = try XCTUnwrap(
            JSONSerialization.jsonObject(with: Data(info.json().utf8)) as? [String: Any])
        XCTAssertEqual(object["model"] as? String, "iPhone17,1")
        XCTAssertEqual(object["locale"] as? String, "fr-FR")
        XCTAssertEqual((object["dataStore"] as? [String: Any])?["persisting"] as? Bool, true)
    }

    func testMachineReadsACString() {
        let tuple: (CChar, CChar, CChar, CChar, CChar, CChar) = (0x69, 0x50, 0x61, 0x64, 0, 0x41)
        XCTAssertEqual(AppInfo.machine(tuple), "iPad")
    }

    func testFileNames() throws {
        let date = Date(timeIntervalSince1970: 1_793_284_800)  // 2026-10-29 14:40:00 UTC
        let utc = try XCTUnwrap(TimeZone(identifier: "UTC"))
        XCTAssertEqual(DiagnosticsFiles.zipName(at: date, timeZone: utc), "ichor-diagnostics-20261029-1440.zip")
        XCTAssertEqual(DiagnosticsFiles.payloadName(at: date, index: 2), "mx-20261029T144000Z-2.json")
    }

    func testOnlyTheNewestPayloadsAreKept() {
        let names = (0..<13).map { String(format: "mx-202610%02dT000000Z-0.json", $0 + 1) } + ["notes.txt"]
        XCTAssertEqual(DiagnosticsFiles.payloads(in: names).first, "mx-20261013T000000Z-0.json")
        XCTAssertEqual(
            DiagnosticsFiles.pruned(names),
            ["mx-20261003T000000Z-0.json", "mx-20261002T000000Z-0.json", "mx-20261001T000000Z-0.json"])
        XCTAssertTrue(DiagnosticsFiles.pruned(Array(names.prefix(4))).isEmpty)
    }

    func testPlatformJSONIsAnArrayOfThePayloads() throws {
        let json = DiagnosticsFiles.platformJSON(payloads: [
            Data(Self.crashPayload.utf8), Data("broken".utf8), Data(Self.hangPayload.utf8),
        ])
        let array = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(json.utf8)) as? [[String: Any]])
        XCTAssertEqual(array.count, 2)
        XCTAssertNotNil(array[0]["crashDiagnostics"])
        XCTAssertEqual(DiagnosticsFiles.platformJSON(payloads: []), "[]")
    }

    func testAttrsJSON() {
        XCTAssertEqual(DiagnosticsFiles.attrsJSON([:]), "{}")
        XCTAssertEqual(
            DiagnosticsFiles.attrsJSON([
                "ms": .number(12), "ratio": .number(0.5), "ok": .bool(false), "kind": .text("hang"),
            ]),
            #"{"kind":"hang","ms":12,"ok":false,"ratio":0.5}"#)
    }
}
