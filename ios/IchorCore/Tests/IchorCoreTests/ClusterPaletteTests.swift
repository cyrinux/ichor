import XCTest
@testable import IchorCore

final class ClusterPaletteTests: XCTestCase {
    /// Built-in seeds, a hue sweep at the slider's vividness, greys and extreme picks.
    private var seeds: [Int] {
        let sweep = stride(from: 0.0, to: 360.0, by: 10.0).map { hslToRGB(hue: $0, saturation: 0.8, lightness: 0.55) }
        let extremes = [
            0xFFFF00, 0x00FFFF, 0x0000FF, 0xFF0000, 0x00FF00, 0xFF00FF, 0x010101, 0xFEFEFE, 0x0A0A3A, 0xF0F0C0,
        ]
        let greys = [0x000000, 0xFFFFFF, 0x808080, 0x7F8082, 0x333333]
        return clusterSeeds + sweep + extremes + greys
    }

    private let appearances: [PaletteAppearance] = [.light, .dark, .trueBlack]

    /// Text on the identity area reads (WCAG AA, ordinary text).
    func testPrimaryContainerTextContrast() {
        for seed in seeds {
            for appearance in appearances {
                let palette = ClusterPalette(seed: seed, appearance: appearance)
                let ratio = contrast(palette.onPrimaryContainer, palette.primaryContainer)
                XCTAssertGreaterThanOrEqual(ratio, 4.5, "\(String(seed, radix: 16)) \(appearance)")
            }
        }
    }

    /// Icons on their containers stand out (WCAG non-text contrast, 3:1).
    func testSecondaryContainerIconContrast() {
        for seed in seeds {
            for appearance in appearances {
                let palette = ClusterPalette(seed: seed, appearance: appearance)
                let ratio = contrast(palette.onSecondaryContainer, palette.secondaryContainer)
                XCTAssertGreaterThanOrEqual(ratio, 3, "\(String(seed, radix: 16)) \(appearance)")
            }
        }
    }

    /// Android's role tones: containers at 90 (light) / 30 (dark), what sits on them at 10 / 90.
    func testRoleTonesMatchAndroid() {
        for seed in clusterSeeds {
            let light = ClusterPalette(seed: seed, appearance: .light)
            XCTAssertEqual(tone(of: light.primaryContainer), 90, accuracy: 1)
            XCTAssertEqual(tone(of: light.onPrimaryContainer), 10, accuracy: 1)
            XCTAssertEqual(tone(of: light.secondaryContainer), 90, accuracy: 1)
            XCTAssertEqual(tone(of: light.onSecondaryContainer), 10, accuracy: 1)
            let dark = ClusterPalette(seed: seed, appearance: .dark)
            XCTAssertEqual(tone(of: dark.primaryContainer), 30, accuracy: 1)
            XCTAssertEqual(tone(of: dark.onPrimaryContainer), 90, accuracy: 1)
            XCTAssertEqual(tone(of: dark.secondaryContainer), 30, accuracy: 1)
            XCTAssertEqual(tone(of: dark.onSecondaryContainer), 90, accuracy: 1)
        }
    }

    /// True black only blackens the page: the containers keep the dark tones.
    func testTrueBlackKeepsDarkContainers() {
        for seed in clusterSeeds {
            XCTAssertEqual(
                ClusterPalette(seed: seed, appearance: .trueBlack), ClusterPalette(seed: seed, appearance: .dark))
        }
    }

    /// Secondary is the quieter role: same hue, less saturated than primary.
    func testSecondaryIsQuieterThanPrimary() {
        for seed in clusterSeeds {
            let palette = ClusterPalette(seed: seed, appearance: .light)
            XCTAssertLessThan(hsl(palette.secondaryContainer).saturation, hsl(palette.primaryContainer).saturation)
            XCTAssertEqual(hsl(palette.primaryContainer).hue, hsl(seed).hue, accuracy: 3)
        }
    }

    func testGreySeedsStayGrey() {
        for seed in [0x808080, 0x000000, 0xFFFFFF, 0x7F8082] {
            for appearance in appearances {
                let palette = ClusterPalette(seed: seed, appearance: appearance)
                for color in [
                    palette.primaryContainer, palette.onPrimaryContainer, palette.secondaryContainer,
                    palette.onSecondaryContainer,
                ] {
                    XCTAssertLessThan(hsl(color).saturation, 0.1, String(seed, radix: 16))
                }
            }
        }
    }

    /// Another seed, another palette: the colors are computed from the seed, nothing kept.
    func testPaletteFollowsTheSeed() {
        XCTAssertNotEqual(
            ClusterPalette(seed: clusterSeeds[0], appearance: .light),
            ClusterPalette(seed: clusterSeeds[1], appearance: .light))
        XCTAssertNotEqual(
            ClusterPalette(seed: clusterSeeds[0], appearance: .light),
            ClusterPalette(seed: clusterSeeds[0], appearance: .dark))
    }
}
