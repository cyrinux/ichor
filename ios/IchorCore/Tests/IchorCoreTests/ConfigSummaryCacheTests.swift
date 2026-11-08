import XCTest
@testable import IchorCore

final class ConfigSummaryCacheTests: XCTestCase {
    private let talos = ConfigSummary(
        current: "lab",
        contexts: [
            ContextSummary(
                name: "lab", fingerprint: "fp-lab", clusterID: "c-lab", endpoints: ["10.0.0.2"],
                roles: ["os:admin"], certNotAfter: 1_900_000_000)
        ])
    private let kube = ConfigSummary(
        current: "edge",
        contexts: [
            ContextSummary(
                name: "edge", kind: ContextKind.kube, fingerprint: "fp-edge", endpoints: ["https://10.0.1.2:6443"],
                namespace: "default", auth: "token")
        ])

    private func record(generation: Int = 0, kube: ConfigSummary? = nil) throws -> ConfigSummaryRecord {
        try ConfigSummaryRecord(
            talosDigest: "t1", kubeDigest: kube == nil ? "" : "k1", maskGeneration: generation,
            summaries: StoredSummaries(talos: talos, kube: kube))
    }

    func testRoundTripsThroughStorage() throws {
        let stored = try record(kube: kube)
        let read = try XCTUnwrap(ConfigSummaryRecord.decode(stored.encoded()))
        XCTAssertEqual(read, stored)
        XCTAssertEqual(read.summaries, StoredSummaries(talos: talos, kube: kube))
    }

    func testHitWhenDigestAndGenerationMatch() throws {
        let cache = try record()
        XCTAssertEqual(cache.knownDigest(.talosconfig, generation: 0), "t1")
        let hit = try summaryFromIfChanged("", kind: .talosconfig, record: cache)
        XCTAssertTrue(hit.cached)
        XCTAssertEqual(hit.summary, talos)
    }

    func testMissOnTextChange() throws {
        let cache = try record()
        // Go parses when the digest differs: its output is the fresh summary.
        let fresh = #"{"current":"lab-2","contexts":[{"name":"lab-2","fingerprint":"fp-2"}]}"#
        let miss = try summaryFromIfChanged(fresh, kind: .talosconfig, record: cache)
        XCTAssertFalse(miss.cached)
        XCTAssertEqual(miss.summary.current, "lab-2")
    }

    func testMissOnMaskGenerationChange() throws {
        let cache = try record(generation: 0)
        XCTAssertEqual(cache.knownDigest(.talosconfig, generation: 41), "")
        let masked = try record(generation: 41)
        XCTAssertEqual(masked.knownDigest(.talosconfig, generation: 0), "")
        XCTAssertEqual(masked.knownDigest(.talosconfig, generation: 42), "")
    }

    func testStoreWithoutSummaryIsNeverAHit() throws {
        let cache = try record()
        XCTAssertEqual(cache.knownDigest(.kubeconfig, generation: 0), "")
        XCTAssertThrowsError(try summaryFromIfChanged("", kind: .kubeconfig, record: cache))
        XCTAssertThrowsError(try summaryFromIfChanged("", kind: .talosconfig, record: nil))
    }

    func testCorruptDataIsAMiss() throws {
        XCTAssertNil(ConfigSummaryRecord.decode(Data("not json".utf8)))
        XCTAssertNil(ConfigSummaryRecord.decode(Data()))
        let badSummary = #"{"talosDigest":"t1","kubeDigest":"","maskGeneration":0,"summaryJSON":"{oops"}"#
        XCTAssertNil(ConfigSummaryRecord.decode(Data(badSummary.utf8)))
    }

    func testStoresOnlyWhenTheMaskDidNotChangeSinceTheParse() throws {
        let parsedUnmasked = try record(generation: 0)
        XCTAssertTrue(parsedUnmasked.shouldStore(currentGeneration: 0, stored: nil))
        // Screenshot mode turned on (or off) while the import was parsing: not stored.
        XCTAssertFalse(parsedUnmasked.shouldStore(currentGeneration: 7, stored: nil))
        XCTAssertFalse(try record(generation: 7).shouldStore(currentGeneration: 0, stored: parsedUnmasked))
    }

    func testDoesNotStoreTheSameRecordAgain() throws {
        let stored = try record()
        XCTAssertFalse(try record().shouldStore(currentGeneration: 0, stored: stored))
        XCTAssertTrue(try record(kube: kube).shouldStore(currentGeneration: 0, stored: stored))
    }
}
