import SwiftUI
import IchorCore

// The building blocks every screen shares, the same set as Android's ui/components. Callers
// pass the text (already localized), so nothing here adds a string.
//
// Rules:
// - Health colours come only from Color.statusOK / statusWarn / statusBad, plus .secondary for
//   what is muted.
// - One StatusPill per header, a HealthDot per row, a Tag for categories. A Tag never takes a
//   health colour unless its value is itself a problem.
// - A tag identical on every row says nothing about the row: drop it from the rows.
// - Numbers are StatTiles, not prose.
// - One bar style: UsageBar, or MeterRow around it; the caller passes the thresholds.
// - Errors use the existing error views, never a plain Text.

/// A row's health as a coloured dot; VoiceOver reads the health.
struct HealthDot: View {
    let health: ServiceHealth
    var size: CGFloat = 10

    var body: some View {
        Circle().fill(health.color).frame(width: size, height: size)
            .accessibilityLabel(Text(health.label))
    }
}

/// A small rounded label: a category, a version (`mono`), or a problem in its status colour.
/// Without a colour it stays neutral (primary text on a light fill).
struct Tag: View {
    let text: String
    let color: Color?
    let mono: Bool

    init(_ text: String, color: Color? = nil, mono: Bool = false) {
        self.text = text
        self.color = color
        self.mono = mono
    }

    var body: some View {
        Text(verbatim: text)
            .font(mono ? .caption.monospaced() : .caption.weight(.medium))
            .padding(.horizontal, 10)
            .padding(.vertical, 4)
            .foregroundStyle(color.map { AnyShapeStyle($0) } ?? AnyShapeStyle(HierarchicalShapeStyle.primary))
            .background(color.map { AnyShapeStyle($0.opacity(0.15)) } ?? AnyShapeStyle(Color(.tertiarySystemFill)), in: Capsule())
    }
}

/// A number with what it counts, under an optional icon; `meter` (a bar, a sparkline) goes below.
struct StatTile: View {
    let icon: String?
    let value: String
    let label: String
    var meter: (() -> AnyView)? = nil

    var body: some View {
        if let meter {
            VStack(alignment: .leading, spacing: 4) {
                stat
                meter()
            }
        } else {
            stat
        }
    }

    private var stat: some View {
        // Centred: on a home summary the icon sits in a container as tall as the two lines.
        HStack(alignment: icon == nil ? .firstTextBaseline : .center, spacing: 8) {
            if let icon {
                StatIcon(systemName: icon)
            }
            // Narrow phones and long translations wrap rather than cut the label short.
            VStack(alignment: .leading, spacing: 1) {
                Text(verbatim: value).font(.headline).monospacedDigit()
                Text(verbatim: label).font(.caption2).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

/// "CPU  ▇▇▇▁▁  42 %": a label on about a quarter of the row, the usage bar, then the value.
struct MeterRow: View {
    let label: String
    let fraction: Double
    let value: String
    var warnAt = 0.75

    var body: some View {
        HStack(spacing: 10) {
            Text(verbatim: label)
                .font(.caption)
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .containerRelativeFrame(.horizontal, alignment: .leading) { width, _ in width * 0.25 }
            UsageBar(fraction: fraction, warnAt: warnAt)
            Text(verbatim: value).font(.caption).monospacedDigit()
        }
        .accessibilityElement(children: .combine)
    }
}

/// One resource in a List: its health dot (or the same room when it has none), a one-line
/// title, a caption tinted with the health only when that needs attention, extra `content`
/// under them and a `trailing` view (a count, a Tag, a chevron).
struct ResourceRow<Trailing: View, Content: View>: View {
    let health: ServiceHealth?
    let title: String
    let subtitle: String?
    let mono: Bool
    let trailing: Trailing
    let content: Content

    private let dotSize: CGFloat = 10

    init(health: ServiceHealth?, title: String, subtitle: String? = nil, mono: Bool = true,
         trailing: Trailing, @ViewBuilder content: () -> Content) {
        self.health = health
        self.title = title
        self.subtitle = subtitle
        self.mono = mono
        self.trailing = trailing
        self.content = content()
    }

    var body: some View {
        HStack(alignment: .center, spacing: 10) {
            if let health {
                HealthDot(health: health, size: dotSize)
            } else {
                Color.clear.frame(width: dotSize, height: dotSize).accessibilityHidden(true)
            }
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: title)
                    .font(mono ? .body.monospaced() : .body)
                    .lineLimit(1)
                    .truncationMode(.middle)
                if let subtitle, !subtitle.isEmpty {
                    Text(verbatim: subtitle).font(.caption).foregroundStyle(subtitleColor)
                }
                content
            }
            Spacer(minLength: 8)
            trailing
        }
    }

    private var subtitleColor: Color {
        guard let health, health.needsAttention else { return .secondary }
        return health.color
    }
}

// `trailing` is a view, `content` the only closure: a trailing closure is always the content.
extension ResourceRow where Trailing == EmptyView, Content == EmptyView {
    init(health: ServiceHealth?, title: String, subtitle: String? = nil, mono: Bool = true) {
        self.init(health: health, title: title, subtitle: subtitle, mono: mono, trailing: EmptyView(), content: { EmptyView() })
    }
}

extension ResourceRow where Content == EmptyView {
    init(health: ServiceHealth?, title: String, subtitle: String? = nil, mono: Bool = true, trailing: Trailing) {
        self.init(health: health, title: title, subtitle: subtitle, mono: mono, trailing: trailing, content: { EmptyView() })
    }
}

extension ResourceRow where Trailing == EmptyView {
    init(health: ServiceHealth?, title: String, subtitle: String? = nil, mono: Bool = true,
         @ViewBuilder content: () -> Content) {
        self.init(health: health, title: title, subtitle: subtitle, mono: mono, trailing: EmptyView(), content: content)
    }
}

/// A Section header: the title, an optional line under it, and the section's one pill.
struct SectionHeader<Pill: View>: View {
    let title: String
    let subtitle: String?
    let pill: Pill

    init(title: String, subtitle: String? = nil, pill: Pill) {
        self.title = title
        self.subtitle = subtitle
        self.pill = pill
    }

    var body: some View {
        HStack(alignment: .firstTextBaseline) {
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: title)
                if let subtitle, !subtitle.isEmpty {
                    // Muted as in a Section header, also when the header heads a row.
                    Text(verbatim: subtitle).font(.caption).foregroundStyle(.secondary).monospacedDigit().textCase(nil)
                }
            }
            Spacer(minLength: 8)
            pill.textCase(nil)
        }
    }
}

extension SectionHeader where Pill == EmptyView {
    init(title: String, subtitle: String? = nil) {
        self.init(title: title, subtitle: subtitle, pill: EmptyView())
    }
}

/// Nothing to show: a symbol, why, and what to do about it.
struct EmptyState<Action: View>: View {
    let systemImage: String?
    let text: String
    let action: Action

    init(systemImage: String?, text: String, @ViewBuilder action: () -> Action) {
        self.systemImage = systemImage
        self.text = text
        self.action = action()
    }

    var body: some View {
        ContentUnavailableView {
            if let systemImage {
                Label { Text(verbatim: text) } icon: { Image(systemName: systemImage) }
            } else {
                Text(verbatim: text)
            }
        } description: {
            EmptyView()
        } actions: {
            action
        }
    }
}

extension EmptyState where Action == EmptyView {
    init(systemImage: String?, text: String) {
        self.init(systemImage: systemImage, text: text, action: { EmptyView() })
    }
}
