import XCTest
@testable import IchorCore

final class SearchTests: XCTestCase {
    private let contexts = [
        ContextSummary(name: "admin@lab", fingerprint: "fp-lab", clusterID: "lablablablablabl"),
        ContextSummary(name: "admin@prod", fingerprint: "fp-prod", clusterID: "prodprodprodprod"),
        ContextSummary(name: "reader@prod", fingerprint: "fp-prod-reader", clusterID: "prodprodprodprod"),
        ContextSummary(name: "admin@legacy", fingerprint: "fp-legacy"),
        ContextSummary(name: "demo", fingerprint: "fp-demo", clusterID: "demodemodemodemo", demo: true),
    ]

    private func store(_ entries: [String: [String: String]]) -> (String, String) -> LastKnownEntry? {
        { fingerprint, key in
            entries[fingerprint]?[key].map { LastKnownEntry(key: key, at: Date(epochMillis: 1_000), json: $0) }
        }
    }

    func testOneEntryPerClusterAndKindTheActiveClusterFirst() {
        let read = store([
            "fp-lab": ["overview": "{}", "pods|*": "[]", "pods": "{}", "workloads": "{}"],
            "fp-prod": ["overview": "{}"],
            "fp-prod-reader": ["cronjobs|*": "[]"],
        ])
        let entries = searchEntries(contexts: contexts, activeContext: "admin@prod", read: read)
        XCTAssertEqual(entries.map(\.cluster), ["prodprodprodprod", "prodprodprodprod", "lablablablablabl", "lablablablablabl", "lablablablablabl"])
        // The list of every namespace before the summary key; one entry per kind.
        XCTAssertEqual(entries.map(\.key), ["overview", "cronjobs|*", "overview", "pods|*", "workloads"])
        // A kind another context of the cluster has is found there.
        XCTAssertEqual(entries[1].context, "reader@prod")
        XCTAssertEqual(entries[0].at, 1_000)
    }

    func testAClusterWithoutIdIsSearchedByFingerprintAndDemoGivesNothing() {
        let read = store(["fp-legacy": ["overview": "{}"], "fp-demo": ["overview": "{}"]])
        let entries = searchEntries(contexts: contexts, activeContext: "demo", read: read)
        XCTAssertEqual(entries.map(\.cluster), ["fp-legacy"])
    }

    func testMissingKeysAreSkipped() {
        XCTAssertEqual(searchEntries(contexts: contexts, activeContext: "admin@lab", read: store([:])), [])
    }

    func testTheActiveClusterGetsItsExtraEntriesWhichReplaceStoredKinds() {
        let read = store(["fp-lab": ["overview": "{}", "pods|*": "[]"]])
        let argo = SearchEntry(cluster: "lablablablablabl", context: "admin@lab", key: "argocd", at: 2_000 as Int64, json: "{}")
        let pods = SearchEntry(cluster: "lablablablablabl", context: "admin@lab", key: "pods", at: 3_000 as Int64, json: "{}")
        let entries = searchEntries(contexts: contexts, activeContext: "admin@lab", read: read, active: [argo, pods])
        XCTAssertEqual(entries.map(\.key), ["overview", "argocd", "pods"])
        XCTAssertEqual(entries.last?.at, 3_000)
    }

    func testSearchKindDropsTheScope() {
        XCTAssertEqual(searchKind(of: "pods|kube-system"), "pods")
        XCTAssertEqual(searchKind(of: "services/node-a"), "services")
        XCTAssertEqual(searchKind(of: "overview"), "overview")
    }

    func testEntriesEncodeAsTheGoCoreReadsThem() throws {
        let json = SearchEntries(entries: [SearchEntry(cluster: "c", context: "x", key: "pods|*", at: 5 as Int64, json: "[]")]).json
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(json.utf8)) as? [String: Any])
        let entry = try XCTUnwrap((object["entries"] as? [[String: Any]])?.first)
        XCTAssertEqual(entry["cluster"] as? String, "c")
        XCTAssertEqual(entry["key"] as? String, "pods|*")
        XCTAssertEqual(entry["at"] as? Int, 5)
        XCTAssertEqual(entry["json"] as? String, "[]")
        XCTAssertEqual(try TalosJSON.decode(SearchEntries.self, from: json).entries.count, 1)
    }

    func testResultsDecodeWithAndWithoutTarget() throws {
        let json = """
        {"results":[
          {"cluster":"lablablablablabl","context":"admin@lab","kind":"pod","namespace":"shop","name":"api-1",
           "detail":"Pod · Running","at":1000,"score":0.8,
           "target":{"cluster":"lablablablablabl","target":"pod","ns":"shop","name":"api-1"}},
          {"cluster":"fp-legacy","context":"admin@legacy","kind":"node","name":"cp-1","detail":"Node","at":2000,"score":0.6}
        ],"total":7}
        """
        let results = try TalosJSON.decode(SearchResults.self, from: json)
        XCTAssertEqual(results.total, 7)
        XCTAssertEqual(results.results.count, 2)
        let pod = results.results[0]
        XCTAssertEqual(pod.target, ShareTarget(cluster: "lablablablablabl", target: .pod, namespace: "shop", name: "api-1"))
        XCTAssertEqual(pod.id, "lablablablablabl|pod|shop|api-1")
        XCTAssertNil(results.results[1].target)
        XCTAssertEqual(results.results[1].namespace, "")
        XCTAssertEqual(try TalosJSON.decode(SearchResults.self, from: #"{"results":[]}"#), .empty)
    }

    func testSectionsPutTheActiveClusterFirst() throws {
        let json = """
        {"results":[
          {"cluster":"b","context":"x@b","kind":"pod","name":"one","detail":"","at":0,"score":1},
          {"cluster":"a","context":"x@a","kind":"pod","name":"two","detail":"","at":0,"score":0.9},
          {"cluster":"b","context":"x@b","kind":"pod","name":"three","detail":"","at":0,"score":0.5}
        ],"total":3}
        """
        let results = try TalosJSON.decode(SearchResults.self, from: json).results
        let sections = searchSections(results, activeCluster: "a")
        XCTAssertEqual(sections.map(\.cluster), ["a", "b"])
        XCTAssertEqual(sections[1].context, "x@b")
        XCTAssertEqual(sections[1].results.map(\.name), ["one", "three"])
        XCTAssertEqual(searchSections(results, activeCluster: "z").map(\.cluster), ["b", "a"])
    }

    func testStaleAfterFiveMinutes() throws {
        let result = try TalosJSON.decode(SearchResult.self, from: #"{"cluster":"a","kind":"pod","name":"p","at":0}"#)
        XCTAssertFalse(searchIsStale(result, now: Date(epochMillis: 299_000)))
        XCTAssertTrue(searchIsStale(result, now: Date(epochMillis: 301_000)))
    }

    func testRecentsKeepTheNewestOnceAndAtMostTen() {
        XCTAssertEqual(searchRecents(adding: "  API ", to: ["web", "api"]), ["API", "web"])
        XCTAssertEqual(searchRecents(adding: " ", to: ["web"]), ["web"])
        let many = (0..<10).map { "q\($0)" }
        XCTAssertEqual(searchRecents(adding: "new", to: many).count, searchRecentLimit)
        XCTAssertEqual(searchRecents(adding: "new", to: many).last, "q8")
    }

    func testGitOpsAppsEncodeTheFieldsTheGoCoreReads() throws {
        let argo = try TalosJSON.decode(ArgoStatus.self, from: """
        {"installed":true,"apps":[{"namespace":"argocd","name":"shop","project":"default","level":"warning","health":"Degraded"}]}
        """)
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(argo.searchJSON.utf8)) as? [String: Any])
        let app = try XCTUnwrap((object["apps"] as? [[String: Any]])?.first)
        XCTAssertEqual(app["name"] as? String, "shop")
        XCTAssertEqual(app["project"] as? String, "default")
        XCTAssertEqual(app["level"] as? String, "warning")
        XCTAssertEqual(app["health"] as? String, "Degraded")

        let flux = try TalosJSON.decode(FluxStatus.self, from: """
        {"installed":true,"apps":[{"kind":"HelmRelease","namespace":"flux-system","name":"podinfo","level":"ok"}]}
        """)
        XCTAssertTrue(flux.searchJSON.contains(#""kind":"HelmRelease""#))
        XCTAssertTrue(flux.searchJSON.contains(#""level":"ok""#))
    }
}
