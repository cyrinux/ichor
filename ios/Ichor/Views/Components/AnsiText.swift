import SwiftUI
import IchorCore

/// The 16 terminal colours as theme colours readable in light and dark mode, and exact
/// (256 and true) colours nudged to 3:1 contrast on the surface (IchorCore AnsiColor.readable).
@MainActor
enum AnsiPalette {
    static func color(_ color: AnsiColor, scheme: ColorScheme) -> Color? {
        switch color {
        case .none:
            return nil
        case .index(let index):
            return indexed(index, scheme: scheme)
        case .rgb:
            guard case .rgb(let r, let g, let b) = color.readable(darkSurface: scheme == .dark) else { return nil }
            return Color(red: Double(r) / 255, green: Double(g) / 255, blue: Double(b) / 255)
        }
    }

    /// Black and white follow the scheme (black on a dark surface would not show); bright
    /// colours are a step lighter in dark mode and darker in light mode.
    private static func indexed(_ index: Int, scheme: ColorScheme) -> Color? {
        let dark = scheme == .dark
        switch index {
        case 0: return dark ? Color.gray : Color.primary
        case 7: return dark ? Color.primary : Color.gray
        case 8: return Color.gray
        case 15: return Color.primary
        case 1...6: return base(index)
        case 9...14: return bright(base(index - 8), dark: dark)
        default: return nil
        }
    }

    /// Red, green, yellow, blue, magenta, cyan.
    private static func base(_ index: Int) -> Color {
        switch index {
        case 1: .statusBad
        case 2: .statusOK
        case 3: .statusWarn
        case 4: .accentColor
        case 5: .purple
        default: .teal
        }
    }

    private static func bright(_ color: Color, dark: Bool) -> Color {
        if #available(iOS 18, *) {
            return color.mix(with: dark ? .white : .black, by: 0.25)
        }
        return color.opacity(0.8)
    }
}

/// A log line with its ANSI colours: `entry.raw` drawn plainly when it had none.
@MainActor
func attributedLine(_ entry: LogEntry, scheme: ColorScheme) -> AttributedString {
    attributedLine(entry.raw, spans: entry.spans, scheme: scheme)
}

/// `text` styled by `spans` (whose texts make it up); the default-coloured runs keep the
/// view's foreground style.
@MainActor
func attributedLine(_ text: String, spans: [LogSpan], scheme: ColorScheme) -> AttributedString {
    guard !spans.isEmpty else { return AttributedString(text) }
    var result = AttributedString()
    for run in ansiRuns(spans) {
        result.append(styled(run, scheme: scheme))
    }
    return result
}

@MainActor
private func styled(_ run: AnsiRun, scheme: ColorScheme) -> AttributedString {
    var text = AttributedString(run.text)
    guard !run.isPlain else { return text }
    // By attribute type: a `text.foregroundColor = …` key path is not Sendable in Swift 6 mode.
    let foreground = AnsiPalette.color(run.fg, scheme: scheme)
    if run.dim {
        text[AttributeScopes.SwiftUIAttributes.ForegroundColorAttribute.self] =
            foreground?.opacity(0.6) ?? Color.secondary
    } else if let foreground {
        text[AttributeScopes.SwiftUIAttributes.ForegroundColorAttribute.self] = foreground
    }
    if let background = AnsiPalette.color(run.bg, scheme: scheme) {
        text[AttributeScopes.SwiftUIAttributes.BackgroundColorAttribute.self] = background.opacity(0.35)
    }
    var intent: InlinePresentationIntent = []
    if run.bold { intent.insert(.stronglyEmphasized) }
    if run.italic { intent.insert(.emphasized) }
    if !intent.isEmpty {
        text[AttributeScopes.FoundationAttributes.InlinePresentationIntentAttribute.self] = intent
    }
    if run.underline {
        text[AttributeScopes.SwiftUIAttributes.UnderlineStyleAttribute.self] = .single
    }
    return text
}
