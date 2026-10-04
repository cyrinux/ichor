import SwiftUI
import IchorCore

/// Spinner / error with retry / content for a LoadState.
struct LoadStateView<T, Content: View>: View {
    let state: LoadState<T>
    let retry: () async -> Void
    @ViewBuilder let content: (T) -> Content

    var body: some View {
        switch state {
        case .loading:
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
        case .failed(let message):
            // "This node's Talos version cannot do that" is information, not a failure.
            if let notice = versionNotice(message) {
                VersionNoticeView(notice: notice, detail: message, retry: retry)
            } else {
                ContentUnavailableView {
                    Label("Request failed", systemImage: "exclamationmark.triangle")
                } description: {
                    Text(message)
                } actions: {
                    Button("Retry") { Task { await retry() } }
                }
            }
        case .loaded(let value, let at, let refreshError):
            content(value)
                .safeAreaInset(edge: .bottom, spacing: 0) { FreshnessFooter(at: at, refreshError: refreshError) }
        }
    }
}

/// The label stays in the primary colour: system green or orange text on a tint of itself
/// is about 2:1, unreadable for many; the dot and the tint carry the colour.
struct StatusPill: View {
    let label: String
    let color: Color

    var body: some View {
        HStack(spacing: 6) {
            Circle().fill(color).frame(width: 8, height: 8).accessibilityHidden(true)
            Text(label).font(.caption.weight(.medium)).foregroundStyle(.primary)
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 4)
        .background(color.opacity(0.15), in: Capsule())
    }
}

/// Status colours readable as text (at least 4.5:1 on light and dark list backgrounds), the
/// same tones as on Android; the system green, orange and red are 2.1 to 3.4:1 on white.
extension ShapeStyle where Self == Color {
    static var statusOK: Color { Color(light: 0x17703C, dark: 0x5BD18B) }
    static var statusWarn: Color { Color(light: 0x8A6100, dark: 0xF2C14E) }
    static var statusBad: Color { Color(light: 0xC0282D, dark: 0xFF7B7B) }
}

extension Color {
    init(light: UInt32, dark: UInt32) {
        self.init(uiColor: UIColor { $0.userInterfaceStyle == .dark ? UIColor(rgb: dark) : UIColor(rgb: light) })
    }
}

private extension UIColor {
    convenience init(rgb: UInt32) {
        self.init(
            red: CGFloat((rgb >> 16) & 0xFF) / 255,
            green: CGFloat((rgb >> 8) & 0xFF) / 255,
            blue: CGFloat(rgb & 0xFF) / 255,
            alpha: 1
        )
    }
}

/// Reads `message` out with VoiceOver: what changed when nothing on screen moves focus to it.
@MainActor
func announce(_ message: String) {
    AccessibilityNotification.Announcement(message).post()
}

extension NodeHealth {
    var label: String {
        switch self {
        case .ready: String(localized: "Ready")
        case .notReady: String(localized: "Not ready")
        case .unreachable: String(localized: "Unreachable")
        }
    }

    var color: Color {
        switch self {
        case .ready: .green
        case .notReady: .orange
        case .unreachable: .red
        }
    }
}

struct UsageBar: View {
    let fraction: Double

    var body: some View {
        ProgressView(value: min(max(fraction, 0), 1))
            .tint(fraction >= 0.9 ? .red : fraction >= 0.75 ? .orange : .green)
    }
}

/// Usage bar with the storage thresholds: orange from 80 %, red from 90 %.
struct UsageLevelBar: View {
    let fraction: Double
    let level: UsageLevel

    init(percent: Double) {
        fraction = usageFraction(percent: percent)
        level = usageLevel(percent: percent)
    }

    init(fraction: Double, level: UsageLevel) {
        self.fraction = fraction
        self.level = level
    }

    var body: some View {
        ProgressView(value: min(max(fraction, 0), 1)).tint(level.color)
    }
}

extension UsageLevel {
    var color: Color {
        switch self {
        case .normal: .green
        case .warning: .orange
        case .critical: .red
        }
    }
}

struct RoleNotice: View {
    let feature: Feature
    let roles: [String]

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("\(feature.localizedLabel) needs \(feature.minimumRole)").font(.headline)
            Text("This talosconfig has \(roleList). Import a talosconfig created with --roles \(feature.minimumRole) to use it.")
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
        .padding()
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.quaternary, in: RoundedRectangle(cornerRadius: 12))
    }

    private var roleList: String {
        roles.isEmpty ? String(localized: "no roles") : roles.joined(separator: ", ")
    }
}

/// "True black" theme: pure black list backgrounds for OLED screens.
struct ThemedBackground: ViewModifier {
    @Environment(AppModel.self) private var model

    func body(content: Content) -> some View {
        if model.theme == .black {
            content.scrollContentBackground(.hidden).background(Color.black)
        } else {
            content
        }
    }
}

extension View {
    func themedBackground() -> some View { modifier(ThemedBackground()) }

    /// An alert titled with `message` (how an action ended, or why it could not), until dismissed.
    func messageAlert(_ message: Binding<String?>) -> some View {
        alert(message.wrappedValue ?? "", isPresented: message.isPresent()) {
            Button("OK") {}
        }
    }
}

extension Text {
    /// A secondary line in a section: loading, empty or failed.
    func note() -> some View {
        font(.callout).foregroundStyle(.secondary)
    }
}

extension Binding {
    /// True while the optional holds a value; set to false (a dismissed alert or dialog), it clears it.
    func isPresent<Wrapped>() -> Binding<Bool> where Value == Wrapped? {
        Binding<Bool>(get: { wrappedValue != nil }, set: { if !$0 { wrappedValue = nil } })
    }
}

/// "Updated 15:42:10 · 2 min ago", re-rendered every 15 s so the age stays true. After a
/// failed refresh: why, and that the data shown is older, in a warning tint.
struct FreshnessFooter: View {
    let at: Date
    var refreshError: String?

    var body: some View {
        TimelineView(.periodic(from: .now, by: 15)) { context in
            let time = at.formatted(date: .omitted, time: .standard)
            let ago = Self.ago(context.date.timeIntervalSince(at))
            Group {
                if let refreshError {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("Couldn't refresh: \(refreshError)").lineLimit(2)
                        Text("Showing data from \(time) (\(ago))")
                    }
                    .foregroundStyle(.statusWarn)
                } else {
                    Text("Updated \(time) · \(ago)").foregroundStyle(.secondary)
                }
            }
            .font(.caption)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal)
            .padding(.vertical, 6)
            .background(.bar)
        }
        .onChange(of: refreshError) { _, error in
            if let error { announce(String(localized: "Couldn't refresh: \(error)")) }
        }
    }

    static func ago(_ seconds: TimeInterval) -> String {
        let s = Int(max(seconds, 0))
        switch s {
        case ..<60: return String(localized: "just now")
        case ..<3_600: return String(localized: "\(s / 60) min ago")
        case ..<86_400: return String(localized: "\(s / 3_600) h ago")
        default: return String(localized: "\(s / 86_400) d ago")
        }
    }
}
