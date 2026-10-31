import XCTest
@testable import IchorCore

final class ClusterTagsTests: XCTestCase {
    func testATypedLineBecomesCleanTags() {
        XCTAssertEqual(normalizeTags("  Prod, eu  PROD,,"), ["prod", "eu"])
        XCTAssertEqual(normalizeTags(" , "), [])
    }

    func testTagsAreCappedInLengthAndNumber() {
        XCTAssertEqual(
            normalizeTags(String(repeating: "a", count: clusterTagMax + 5)),
            [String(repeating: "a", count: clusterTagMax)])
        let many = (1...(clusterTagsMax + 3)).map { "t\($0)" }.joined(separator: ",")
        XCTAssertEqual(normalizeTags(many).count, clusterTagsMax)
    }

    func testRemovedClustersForgetTheirTags() {
        let saved = ["fp-a": ["prod"], "fp-gone": ["staging"]]
        XCTAssertEqual(keepClusterTags(saved: saved, fingerprints: ["fp-a", ""]), ["fp-a": ["prod"]])
    }

    func testAClusterHasTheTagsOfAllItsContexts() {
        let saved = ["fp-a": ["prod", "eu"], "fp-b": ["eu", "edge"]]
        XCTAssertEqual(clusterTagsOf(saved: saved, fingerprints: ["fp-a", "fp-b", "fp-none"]), ["prod", "eu", "edge"])
    }

    func testEveryContextOfTheClusterGetsTheTags() {
        let lab = ContextSummary(name: "admin@lab", fingerprint: "fp-lab", clusterID: "c-lab")
        let reader = ContextSummary(name: "reader@lab", fingerprint: "fp-reader", clusterID: "c-lab")
        let other = ContextSummary(name: "other", fingerprint: "fp-other")
        XCTAssertEqual(clusterFingerprints(of: reader, contexts: [lab, reader, other]), ["fp-lab", "fp-reader"])
    }

    func testSuggestionsAreTheOtherClustersTagsNotYetTyped() {
        let saved = ["fp-a": ["prod"], "fp-b": ["staging", "eu"], "fp-c": ["eu"]]
        XCTAssertEqual(tagSuggestions(saved: saved, own: ["fp-a"], typed: "EU"), ["staging"])
        XCTAssertEqual(tagSuggestions(saved: saved, own: ["fp-c"], typed: "eu"), ["prod", "staging"])
    }

    private func summary() throws -> FleetSummary {
        let json = #"""
            {"clusters":[{"key":"a","tone":"ok"},{"key":"b","tone":"ok"},{"key":"c","tone":"ok"}],
             "worst":"ok",
             "groups":[{"tag":"prod","keys":["a"]},{"tag":"eu","keys":["a","b"]},{"tag":"","keys":["c"]}]}
            """#
        return try JSONDecoder().decode(FleetSummary.self, from: Data(json.utf8))
    }

    func testNoSelectionShowsEveryRow() throws {
        let fleet = try summary()
        XCTAssertEqual(fleetRowsWithTags(fleet.clusters, groups: fleet.groups, selected: []), fleet.clusters)
    }

    func testTheSelectionIsAnOrAndKeepsTheRanking() throws {
        let fleet = try summary()
        XCTAssertEqual(
            fleetRowsWithTags(fleet.clusters, groups: fleet.groups, selected: ["", "prod"]).map(\.key), ["a", "c"])
    }

    func testATagNoLongerUsedDoesNotHideTheFleet() throws {
        let fleet = try summary()
        XCTAssertEqual(activeTagFilter(groups: fleet.groups, selected: ["gone"]), [])
        XCTAssertEqual(fleetRowsWithTags(fleet.clusters, groups: fleet.groups, selected: ["gone"]), fleet.clusters)
        XCTAssertEqual(
            fleetRowsWithTags(fleet.clusters, groups: fleet.groups, selected: ["gone", "eu"]).map(\.key), ["a", "b"])
    }

    func testTheFilterIsStoredWithTheUntaggedChoice() {
        XCTAssertEqual(decodeTagFilter(encodeTagFilter(["", "prod"])), ["", "prod"])
        XCTAssertEqual(decodeTagFilter(encodeTagFilter([""])), [""])
        XCTAssertEqual(decodeTagFilter(""), [])
        XCTAssertEqual(decodeTagFilter("not json"), [])
    }

    func testBackupRoundTripAndCheck() throws {
        let clusters = backupClusters(
            fingerprints: ["aaaa", "bbbb"], names: [:], colors: [:], kubeServers: [:],
            tags: ["aaaa": ["prod", "eu"], "gone": ["old"]])
        XCTAssertEqual(clusters["aaaa"]?.tags, ["prod", "eu"])
        XCTAssertNil(clusters["bbbb"]?.tags)

        let data = try JSONEncoder().encode(clusters)
        let decoded = try JSONDecoder().decode([String: BackupCluster].self, from: data)
        XCTAssertEqual(restoredClusters(decoded, fingerprints: ["aaaa", "bbbb"]).tags, ["aaaa": ["prod", "eu"]])

        let messy = restoredClusters(["aaaa": BackupCluster(tags: [" PROD ", "prod", ""])], fingerprints: ["aaaa"])
        XCTAssertEqual(messy.tags, ["aaaa": ["prod"]])
        // An older backup has no tags.
        let older = try JSONDecoder().decode(BackupCluster.self, from: Data(#"{"name":"Home"}"#.utf8))
        XCTAssertEqual(restoredClusters(["aaaa": older], fingerprints: ["aaaa"]).tags, [:])
    }
}
