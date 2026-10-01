import XCTest
@testable import TalosdevMobileCore

final class ClusterTests: XCTestCase {
    private let three = ConfigSummary(current: "a", contexts: [ContextSummary(name: "a"), ContextSummary(name: "b"), ContextSummary(name: "c")])

    func testAdjacentContextStopsAtBothEnds() {
        XCTAssertEqual(three.adjacentContext(to: "a", step: 1), "b")
        XCTAssertEqual(three.adjacentContext(to: "c", step: -1), "b")
        XCTAssertNil(three.adjacentContext(to: "c", step: 1))
        XCTAssertNil(three.adjacentContext(to: "a", step: -1))
        XCTAssertNil(three.adjacentContext(to: "b", step: 0))
        XCTAssertNil(three.adjacentContext(to: "gone", step: 1))
    }

    func testActiveIndexAfterRemoval() {
        XCTAssertEqual(ConfigSummary.activeIndexAfterRemoval(active: 0, removed: 2, remaining: 3), 0)
        XCTAssertEqual(ConfigSummary.activeIndexAfterRemoval(active: 2, removed: 0, remaining: 3), 1)
        // The active one is removed: its neighbour, the one before it for the last.
        XCTAssertEqual(ConfigSummary.activeIndexAfterRemoval(active: 1, removed: 1, remaining: 3), 1)
        XCTAssertEqual(ConfigSummary.activeIndexAfterRemoval(active: 3, removed: 3, remaining: 3), 2)
    }

    func testFingerprintDecodesAndDefaultsToEmpty() throws {
        let json = #"{"current":"a","contexts":[{"name":"a","fingerprint":"abcdefgh","endpoints":["10.0.0.1"]},{"name":"b"}]}"#
        let summary = try JSONDecoder().decode(ConfigSummary.self, from: Data(json.utf8))
        XCTAssertEqual(summary.contexts.map(\.fingerprint), ["abcdefgh", ""])
    }

    func testColorsAreDistinctKeptAndPruned() {
        let fresh = assignClusterColors(saved: [:], fingerprints: ["a", "b", "c"])
        XCTAssertEqual(fresh["a"], defaultClusterSeed)
        XCTAssertEqual(Set(fresh.values).count, 3)

        let kept = assignClusterColors(saved: ["b": defaultClusterSeed, "c": 0x123456, "gone": clusterSeeds[2]], fingerprints: ["a", "", "b", "c", "a"])
        XCTAssertEqual(kept, ["a": clusterSeeds[1], "b": defaultClusterSeed, "c": 0x123456])

        let many = (0..<clusterSeeds.count + 2).map { "c\($0)" }
        let all = assignClusterColors(saved: [:], fingerprints: many)
        XCTAssertEqual(all.count, many.count)
        XCTAssertEqual(Set(all.values), Set(clusterSeeds))
    }

    func testHSLRoundTrips() {
        for seed in clusterSeeds {
            let (h, s, l) = hsl(seed)
            let back = hslToRGB(hue: h, saturation: s, lightness: l)
            for shift in [16, 8, 0] {
                XCTAssertLessThanOrEqual(abs((seed >> shift & 0xFF) - (back >> shift & 0xFF)), 1)
            }
        }
        XCTAssertEqual(hsl(0x808080).saturation, 0)
    }

    /// Whatever the main color, the accent reads on the system backgrounds (WCAG AA).
    func testAccentTonesAreReadable() {
        let hues = stride(from: 0.0, to: 360.0, by: 15.0).map { hslToRGB(hue: $0, saturation: 0.8, lightness: 0.55) }
        for seed in clusterSeeds + hues + [0x808080] {
            let light = tonalColor(seed: seed, tone: lightAccentTone)
            let dark = tonalColor(seed: seed, tone: darkAccentTone)
            XCTAssertEqual(tone(of: light), lightAccentTone, accuracy: 1)
            XCTAssertEqual(tone(of: dark), darkAccentTone, accuracy: 1)
            XCTAssertGreaterThanOrEqual(contrast(light, 0xFFFFFF), 4.5)
            XCTAssertGreaterThanOrEqual(contrast(dark, 0x000000), 4.5)
        }
    }
}
