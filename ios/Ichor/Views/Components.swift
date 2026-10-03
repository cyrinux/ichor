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

struct StatusPill: View {
    let label: String
    let color: Color

    var body: some View {
        HStack(spacing: 6) {
            Circle().fill(color).frame(width: 8, height: 8)
            Text(label).font(.caption.weight(.medium))
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 4)
        .foregroundStyle(color)
        .background(color.opacity(0.15), in: Capsule())
    }
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
                    .foregroundStyle(.orange)
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
