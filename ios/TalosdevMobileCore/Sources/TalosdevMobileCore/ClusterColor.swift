import Foundation

// A cluster's main color and the tones derived from it. Same rules and numbers as Android
// (model/ClusterColor.kt), so a cluster looks the same on both.

/// Main colors (0xRRGGBB) handed out to clusters as they are imported, far apart in hue so
/// two clusters are told apart at a glance. The first is the Talos orange.
public let clusterSeeds: [Int] = [
    0xFF7A45, // orange
    0x2A78D6, // blue
    0x2E9E6B, // green
    0x8E5BD9, // purple
    0xD6336C, // pink
    0x0E9AA7, // teal
    0xC29A12, // yellow
    0x5C6BC0, // indigo
]

public let defaultClusterSeed = clusterSeeds[0]

/// The color of each cluster of `fingerprints` (ContextSummary.fingerprint): the saved one
/// when it has one, else the first seed no other cluster uses (they repeat past that many
/// clusters). Clusters that are gone are dropped.
public func assignClusterColors(saved: [String: Int], fingerprints: [String]) -> [String: Int] {
    var known: [String] = []
    for fingerprint in fingerprints where !fingerprint.isEmpty && !known.contains(fingerprint) {
        known.append(fingerprint)
    }
    var colors = saved.filter { known.contains($0.key) }
    for fingerprint in known where colors[fingerprint] == nil {
        let used = Set(colors.values)
        colors[fingerprint] = clusterSeeds.first { !used.contains($0) } ?? clusterSeeds[colors.count % clusterSeeds.count]
    }
    return colors
}

/// Tone (see `tonalColor`) of the accent color in light and in dark mode: Android's primary.
public let lightAccentTone = 40.0
public let darkAccentTone = 75.0

/// Below this HSL saturation a main color counts as grey.
private let greySaturation = 0.1

/// Hue (degrees), saturation and lightness (0...1) of a 0xRRGGBB color.
public func hsl(_ rgb: Int) -> (hue: Double, saturation: Double, lightness: Double) {
    let r = Double(rgb >> 16 & 0xFF) / 255, g = Double(rgb >> 8 & 0xFF) / 255, b = Double(rgb & 0xFF) / 255
    let high = max(r, g, b), low = min(r, g, b)
    let delta = high - low
    let lightness = (high + low) / 2
    guard delta > 0 else { return (0, 0, lightness) }
    let saturation = delta / (1 - abs(2 * lightness - 1))
    let sector: Double
    if high == r {
        sector = ((g - b) / delta).truncatingRemainder(dividingBy: 6)
    } else if high == g {
        sector = (b - r) / delta + 2
    } else {
        sector = (r - g) / delta + 4
    }
    let hue = (sector * 60).truncatingRemainder(dividingBy: 360)
    return (hue < 0 ? hue + 360 : hue, saturation, lightness)
}

public func hslToRGB(hue: Double, saturation: Double, lightness: Double) -> Int {
    let chroma = (1 - abs(2 * lightness - 1)) * saturation
    var wrapped = hue.truncatingRemainder(dividingBy: 360)
    if wrapped < 0 { wrapped += 360 }
    let sector = wrapped / 60
    let x = chroma * (1 - abs(sector.truncatingRemainder(dividingBy: 2) - 1))
    let (r, g, b): (Double, Double, Double)
    switch Int(sector) {
    case 0: (r, g, b) = (chroma, x, 0)
    case 1: (r, g, b) = (x, chroma, 0)
    case 2: (r, g, b) = (0, chroma, x)
    case 3: (r, g, b) = (0, x, chroma)
    case 4: (r, g, b) = (x, 0, chroma)
    default: (r, g, b) = (chroma, 0, x)
    }
    let m = lightness - chroma / 2
    func channel(_ v: Double) -> Int { min(max(Int(((v + m) * 255).rounded()), 0), 255) }
    return channel(r) << 16 | channel(g) << 8 | channel(b)
}

/// Relative luminance (WCAG) of a 0xRRGGBB color, 0...1.
public func luminance(_ rgb: Int) -> Double {
    func linear(_ channel: Int) -> Double {
        let c = Double(channel) / 255
        return c <= 0.04045 ? c / 12.92 : pow((c + 0.055) / 1.055, 2.4)
    }
    return 0.2126 * linear(rgb >> 16 & 0xFF) + 0.7152 * linear(rgb >> 8 & 0xFF) + 0.0722 * linear(rgb & 0xFF)
}

/// Perceived lightness (CIE L*, 0...100) of a 0xRRGGBB color.
public func tone(of rgb: Int) -> Double {
    let y = luminance(rgb)
    return y > 216.0 / 24389.0 ? 116 * cbrt(y) - 16 : y * 24389.0 / 27.0
}

/// WCAG contrast ratio between two 0xRRGGBB colors (1...21).
public func contrast(_ a: Int, _ b: Int) -> Double {
    let high = max(luminance(a), luminance(b)), low = min(luminance(a), luminance(b))
    return (high + 0.05) / (low + 0.05)
}

/// The color of `seed`'s hue whose perceived lightness is `tone` (0 black, 100 white), so
/// that it reads as well whatever the hue (plain HSL lightness does not: yellow at 50 % is
/// far brighter than blue at 50 %). Found by bisection: L* grows with the HSL lightness.
public func tonalColor(seed: Int, tone target: Double) -> Int {
    if target <= 0 { return 0x000000 }
    if target >= 100 { return 0xFFFFFF }
    let (hue, saturation, _) = hsl(seed)
    // A grey (iOS's color picker offers them) stays grey: its hue is meaningless.
    let clamped = saturation < greySaturation ? 0 : min(max(saturation, 0.45), 0.9)
    var low = 0.0, high = 1.0
    for _ in 0..<16 {
        let mid = (low + high) / 2
        if tone(of: hslToRGB(hue: hue, saturation: clamped, lightness: mid)) < target { low = mid } else { high = mid }
    }
    return hslToRGB(hue: hue, saturation: clamped, lightness: (low + high) / 2)
}
