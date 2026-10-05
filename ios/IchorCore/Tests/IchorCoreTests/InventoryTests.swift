import XCTest
@testable import IchorCore

final class InventoryTests: XCTestCase {
    // Shaped like the Go core's ClusterInventory (demo data), including Go's null slices.
    private let json = #"""
    {"at":1790000000000,"nodes":5,"answered":4,"apps":[
      {"id":"cilium","name":"Cilium","category":"networking","icon":"cilium","known":true,"system":false,
       "version":"v1.18.2","drift":true,"unpinned":false,"namespaces":["kube-system"],
       "nodes":["192.0.2.20","192.0.2.21"],"containers":3,"running":3,"memory":641728512,
       "images":[{"repo":"quay.io/cilium/cilium","tag":"v1.18.2","containers":1},
                 {"repo":"quay.io/cilium/cilium","tag":"v1.18.1","containers":1},
                 {"repo":"quay.io/cilium/operator-generic","tag":"v1.18.2","containers":1}],
       "pods":[{"namespace":"kube-system","pod":"cilium-8h2kd","node":"192.0.2.20",
                "containers":[{"name":"cilium-agent","image":"quay.io/cilium/cilium:v1.18.2","status":"CONTAINER_RUNNING","memory":224395264}]}]},
      {"id":"coredns","name":"CoreDNS","category":"system","icon":"coredns","known":true,"system":true,
       "version":"v1.12.3","drift":false,"unpinned":false,"namespaces":["kube-system"],"nodes":["192.0.2.10"],
       "containers":2,"running":2,"memory":50331648,
       "images":[{"repo":"registry.k8s.io/coredns/coredns","tag":"v1.12.3","containers":2}],"pods":null},
      {"id":"grafana","name":"Grafana","category":"observability","icon":"grafana","known":true,"system":false,
       "version":"12.2.0","drift":false,"unpinned":false,"namespaces":["monitoring"],"nodes":["192.0.2.21"],
       "containers":2,"running":2,"memory":104857600,
       "images":[{"repo":"docker.io/grafana/grafana","tag":"12.2.0","containers":1}],"pods":[]},
      {"id":"kimai","name":"Kimai","category":"productivity","remoteIcon":"kimai","known":true,"system":false,
       "version":"2.40","drift":false,"unpinned":false,"namespaces":["tools"],"nodes":["192.0.2.20"],
       "containers":1,"running":1,"memory":1,"images":[],"pods":[]},
      {"id":"nightly-backup","name":"nightly-backup","category":"other","known":false,"system":false,
       "version":"","drift":false,"unpinned":false,"namespaces":["tools"],"nodes":null,
       "containers":1,"running":0,"memory":0,
       "images":[{"repo":"","tag":"","digest":"sha256:0123456789abcdef0123","containers":1}],"pods":null},
      {"id":"uptime-kuma","name":"Uptime Kuma","category":"observability","icon":"uptime-kuma","known":true,"system":false,
       "version":"latest","drift":false,"unpinned":true,"namespaces":["default"],"nodes":["192.0.2.20"],
       "containers":1,"running":1,"memory":10,"images":[{"repo":"docker.io/louislam/uptime-kuma","tag":"latest","containers":1}],"pods":[]}
    ]}
    """#

    private func load() throws -> ClusterInventory {
        try TalosJSON.decode(ClusterInventory.self, from: json)
    }

    func testDecodeGoJSON() throws {
        let inventory = try load()
        XCTAssertEqual(inventory.at, 1_790_000_000_000)
        XCTAssertEqual(inventory.nodes, 5)
        XCTAssertEqual(inventory.answered, 4)
        XCTAssertEqual(inventory.apps.count, 6)
        let cilium = inventory.apps[0]
        XCTAssertEqual(cilium.icon, "cilium")
        XCTAssertNil(cilium.remoteIcon)
        XCTAssertEqual(cilium.category, .networking)
        XCTAssertEqual(cilium.memory, 641_728_512)
        XCTAssertEqual(cilium.pods.first?.containers.first?.isRunning, true)
        XCTAssertEqual(inventory.apps[1].pods, [])
        XCTAssertEqual(inventory.apps[4].nodes, [])
        XCTAssertEqual(inventory.apps[4].images.first?.digest, "sha256:0123456789abcdef0123")
        XCTAssertEqual(inventory.apps[3].remoteIcon, "kimai")
    }

    func testUnknownCategoryIsOther() throws {
        let app = try TalosJSON.decode(InventoryApp.self, from: #"{"id":"x","name":"X","category":"quantum"}"#)
        XCTAssertEqual(app.category, .other)
        XCTAssertEqual(app.images, [])
        XCTAssertFalse(app.needsAttention)
    }

    func testAttention() throws {
        let apps = try load().apps
        XCTAssertEqual(apps.filter(\.needsAttention).map(\.id), ["cilium", "uptime-kuma"])
    }

    func testSections() throws {
        let sections = InventorySections(try load().apps)
        XCTAssertEqual(sections.main.map(\.id), ["cilium", "grafana", "kimai", "uptime-kuma"])
        XCTAssertEqual(sections.system.map(\.id), ["coredns"])
        XCTAssertEqual(sections.unrecognised.map(\.id), ["nightly-backup"])
    }

    func testSearch() throws {
        let apps = try load().apps
        XCTAssertEqual(filterApps(apps, query: "GRAF", filter: .all).map(\.id), ["grafana"])
        // Namespaces and image repositories match too.
        XCTAssertEqual(filterApps(apps, query: "kube-system", filter: .all).map(\.id), ["cilium", "coredns"])
        XCTAssertEqual(filterApps(apps, query: "louislam", filter: .all).map(\.id), ["uptime-kuma"])
        XCTAssertEqual(filterApps(apps, query: "uptime-k", filter: .all).map(\.id), ["uptime-kuma"])
        XCTAssertEqual(filterApps(apps, query: "  ", filter: .all).count, 6)
    }

    func testFilters() throws {
        let apps = try load().apps
        XCTAssertEqual(filterApps(apps, query: "", filter: .attention).map(\.id), ["cilium", "uptime-kuma"])
        XCTAssertEqual(filterApps(apps, query: "", filter: .category(.observability)).map(\.id), ["grafana", "uptime-kuma"])
        XCTAssertEqual(filterApps(apps, query: "kuma", filter: .category(.networking)), [])
    }

    func testCategoryCounts() throws {
        let counts = categoryCounts(try load().apps)
        XCTAssertEqual(counts.map(\.category), [.system, .networking, .observability, .productivity, .other])
        XCTAssertEqual(counts.map(\.count), [1, 1, 2, 1, 1])
    }

    func testOverviewTiles() throws {
        let apps = try load().apps
        // Non-system apps; bundled icons first, keeping the name order.
        var tiles = overviewTiles(apps, limit: 3, remoteIcons: false)
        XCTAssertEqual(tiles.shown.map(\.id), ["cilium", "grafana", "uptime-kuma"])
        XCTAssertEqual(tiles.rest, 2)
        tiles = overviewTiles(apps, limit: 4, remoteIcons: true)
        XCTAssertEqual(tiles.shown.map(\.id), ["cilium", "grafana", "kimai", "uptime-kuma"])
        XCTAssertEqual(tiles.rest, 1)
        XCTAssertEqual(overviewTiles(apps, limit: 6, remoteIcons: false).rest, 0)
    }

    func testIconSource() throws {
        let apps = try load().apps
        XCTAssertEqual(apps[0].iconSource(remoteIcons: true), .bundled("cilium"))
        XCTAssertEqual(apps[3].iconSource(remoteIcons: false), .monogram)
        XCTAssertEqual(apps[3].iconSource(remoteIcons: true), .remote("kimai"))
        XCTAssertEqual(apps[4].iconSource(remoteIcons: true), .monogram)
    }

    func testCustomIconSource() throws {
        let png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII="
        XCTAssertEqual(customIconSource("data:image/png;base64,\(png)", remoteIcons: false), .inline(Data(base64Encoded: png)!))
        XCTAssertEqual(customIconSource("https://a.example/w.png", remoteIcons: true), .url(URL(string: "https://a.example/w.png")!))
        // A URL is a download: only when the user allowed them.
        XCTAssertNil(customIconSource("https://a.example/w.png", remoteIcons: false))
        let big = Data(count: customIconMaxBytes + 1).base64EncodedString()
        for bad in ["grafana", "http://a.example/w.png", "https://u:p@a.example/w.png", "https:///w.png",
                    "file:///etc/passwd", "data:image/svg+xml;base64,PHN2Zy8+", "data:image/png;base64,***",
                    "data:image/png;base64,\(big)"] {
            XCTAssertNil(customIconSource(bad, remoteIcons: true), bad)
        }
        // The app's own icon wins over its catalog one; an invalid one falls back to it.
        let app = try TalosJSON.decode(InventoryApp.self, from: #"{"id":"x","name":"X","icon":"cilium","iconUrl":"https://a.example/w.png"}"#)
        XCTAssertEqual(app.iconSource(remoteIcons: true), .url(URL(string: "https://a.example/w.png")!))
        XCTAssertEqual(app.iconSource(remoteIcons: false), .bundled("cilium"))
    }

    func testIconSlugValidation() throws {
        XCTAssertTrue(isValidIconSlug("cilium"))
        XCTAssertTrue(isValidIconSlug("0-a"))
        XCTAssertFalse(isValidIconSlug("../../etc/passwd"))
        XCTAssertFalse(isValidIconSlug("cilium/night"))
        XCTAssertFalse(isValidIconSlug("Cilium"))
        XCTAssertFalse(isValidIconSlug(""))
        // A bundled name that is not a slug is ignored: monogram (or the remote icon).
        let bad = try TalosJSON.decode(InventoryApp.self, from: #"{"id":"x","name":"X","icon":"../x","remoteIcon":"kimai"}"#)
        XCTAssertEqual(bad.iconSource(remoteIcons: false), .monogram)
        XCTAssertEqual(bad.iconSource(remoteIcons: true), .remote("kimai"))
    }

    func testRemoteIconURL() {
        XCTAssertEqual(remoteIconURL(slug: "kimai")?.absoluteString,
                       "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons/webp/kimai.webp")
        XCTAssertNotNil(remoteIconURL(slug: "home-assistant-2"))
        XCTAssertNil(remoteIconURL(slug: ""))
        XCTAssertNil(remoteIconURL(slug: "-kimai"))
        XCTAssertNil(remoteIconURL(slug: "Kimai"))
        XCTAssertNil(remoteIconURL(slug: "../secrets"))
        XCTAssertNil(remoteIconURL(slug: "kimai?x=1"))
        XCTAssertNil(remoteIconURL(slug: "é"))
        XCTAssertNotNil(remoteIconURL(slug: "a" + String(repeating: "b", count: 80)))
        XCTAssertNil(remoteIconURL(slug: "a" + String(repeating: "b", count: 81)))
    }

    func testMonogram() {
        XCTAssertEqual(monogramInitials("Uptime Kuma"), "UK")
        XCTAssertEqual(monogramInitials("nightly-backup"), "NB")
        XCTAssertEqual(monogramInitials("Grafana"), "GR")
        XCTAssertEqual(monogramInitials("x"), "X")
        XCTAssertEqual(monogramInitials("  "), "?")
        XCTAssertEqual(monogramInitials("my_app.server"), "MA")
        XCTAssertEqual(monogramInitials("ghcr.io/foo"), "GI")
        XCTAssertEqual(monogramInitials("(beta) app"), "BA")
    }

    func testMonogramHue() {
        // FNV-1a of the UTF-8 bytes, like the Android app.
        XCTAssertEqual(monogramHue("a"), 340)
        XCTAssertEqual(monogramHue("cilium"), 320)
        XCTAssertEqual(monogramHue("nightly-backup"), 191)
        XCTAssertEqual(monogramHue(""), 61)
        XCTAssert((0..<360).contains(monogramHue("ж")))
    }

    func testDrift() throws {
        let cilium = try load().apps[0]
        XCTAssertEqual(cilium.versionCount, 2)
        XCTAssertEqual(cilium.driftingRepos, ["quay.io/cilium/cilium"])
        XCTAssertEqual(try load().apps[2].versionCount, 1)
    }

    func testShortDigest() {
        XCTAssertEqual(shortDigest("sha256:0123456789abcdef0123"), "sha256:0123456789ab")
        XCTAssertEqual(shortDigest("0123456789abcdef"), "0123456789ab")
    }
}
