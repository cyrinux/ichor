import Foundation

// The platform-free half of drawing a log line's ANSI colours (LogSpan, from the Go core): the
// app maps AnsiColor to a SwiftUI Color for the current scheme (Views/Components/AnsiText.swift).

/// A colour of a styled log run: one of the 16 terminal colours, an exact one (256 and true
/// colours), or the text's default.
public enum AnsiColor: Equatable, Hashable, Sendable {
    case none
    /// 0–7 normal, 8–15 bright: black, red, green, yellow, blue, magenta, cyan, white.
    case index(Int)
    /// 0–255 each.
    case rgb(r: Int, g: Int, b: Int)

    /// From a span's index and "#rrggbb": the exact colour wins, an index outside 0–15 is the default.
    public init(index: Int, hex: String) {
        if let rgb = Self.parseHex(hex) {
            self = rgb
        } else if (0...15).contains(index) {
            self = .index(index)
        } else {
            self = .none
        }
    }

    private static func parseHex(_ hex: String) -> AnsiColor? {
        guard hex.count == 7, hex.hasPrefix("#"), let value = Int(hex.dropFirst(), radix: 16) else { return nil }
        return .rgb(r: (value >> 16) & 0xFF, g: (value >> 8) & 0xFF, b: value & 0xFF)
    }
}

/// A run of a log line in one style.
public struct AnsiRun: Equatable, Sendable {
    public let text: String
    public let fg: AnsiColor
    public let bg: AnsiColor
    public let bold: Bool
    public let dim: Bool
    public let italic: Bool
    public let underline: Bool

    public init(
        _ text: String, fg: AnsiColor = .none, bg: AnsiColor = .none, bold: Bool = false, dim: Bool = false,
        italic: Bool = false, underline: Bool = false
    ) {
        self.text = text
        self.fg = fg
        self.bg = bg
        self.bold = bold
        self.dim = dim
        self.italic = italic
        self.underline = underline
    }

    /// No colour nor style: drawn as plain text.
    public var isPlain: Bool { fg == .none && bg == .none && !bold && !dim && !italic && !underline }
}

/// The runs to draw for `spans` (empty ones dropped).
public func ansiRuns(_ spans: [LogSpan]) -> [AnsiRun] {
    spans.compactMap { span in
        guard !span.t.isEmpty else { return nil }
        return AnsiRun(
            span.t, fg: AnsiColor(index: span.fg, hex: span.fgRgb), bg: AnsiColor(index: span.bg, hex: span.bgRgb),
            bold: span.b, dim: span.d, italic: span.i, underline: span.u)
    }
}

/// WCAG relative luminance of an sRGB colour (components 0–255), 0 (black) to 1 (white).
public func relativeLuminance(r: Int, g: Int, b: Int) -> Double {
    func linear(_ c: Int) -> Double {
        let v = Double(min(max(c, 0), 255)) / 255
        return v <= 0.039_28 ? v / 12.92 : pow((v + 0.055) / 1.055, 2.4)
    }
    return 0.2126 * linear(r) + 0.7152 * linear(g) + 0.0722 * linear(b)
}

/// WCAG contrast ratio of two luminances, 1 to 21.
public func contrastRatio(_ a: Double, _ b: Double) -> Double {
    (max(a, b) + 0.05) / (min(a, b) + 0.05)
}

/// Luminance of the surfaces a log is drawn on: white in light mode, iOS's dark grouped grey.
public let logLightSurfaceLuminance = 1.0
public let logDarkSurfaceLuminance = relativeLuminance(r: 0x1C, g: 0x1C, b: 0x1E)

/// The contrast a log colour needs on its surface (WCAG's for large text and UI parts).
public let logMinimumContrast = 3.0

public extension AnsiColor {
    /// An exact colour readable on the light or dark surface: when its contrast is under 3:1 it
    /// is mixed toward black (light) or white (dark) until it is not, keeping its hue. Indexed
    /// colours and the default come back as they are (the app maps them to theme colours).
    func readable(darkSurface: Bool) -> AnsiColor {
        guard case .rgb(let r, let g, let b) = self else { return self }
        let surface = darkSurface ? logDarkSurfaceLuminance : logLightSurfaceLuminance
        let target = darkSurface ? 255.0 : 0.0
        for step in 0...10 {
            let amount = Double(step) / 10
            let mixed = [r, g, b].map { Int((Double($0) + (target - Double($0)) * amount).rounded()) }
            if contrastRatio(relativeLuminance(r: mixed[0], g: mixed[1], b: mixed[2]), surface) >= logMinimumContrast {
                return .rgb(r: mixed[0], g: mixed[1], b: mixed[2])
            }
        }
        return darkSurface ? .rgb(r: 255, g: 255, b: 255) : .rgb(r: 0, g: 0, b: 0)
    }
}
