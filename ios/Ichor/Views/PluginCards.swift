import SwiftUI
import IchorCore

// The render tree's primitives (docs/plugins.md "Render tree"), drawn once for every plugin;
// same set as Android's ui/plugins/PluginCards.kt. Colours come from the status tokens only;
// labels and values arrive translated from Go, so they are shown verbatim.

/// A plugin tone's colour: the status tokens, the accent for info, muted otherwise.
func pluginToneColor(_ tone: String) -> Color {
    switch tone {
    case "ok": Color.statusOK
    case "warning": Color.statusWarn
    case "critical": Color.statusBad
    case "info": Color.accentColor
    default: Color.secondary
    }
}

/// The plugin's symbol: the data service's when its icon names one (kube-green), else a generic one.
struct PluginIcon: View {
    let item: PluginDetectItem
    var size: CGFloat = 28

    var body: some View {
        if let kind = item.dataServiceKind {
            KindIcon(kind: kind, app: nil, size: size)
        } else {
            Image(systemName: "puzzlepiece.extension")
                .font(.system(size: size * 0.5))
                .foregroundStyle(.secondary)
                .frame(width: size, height: size)
                .background(.quaternary, in: RoundedRectangle(cornerRadius: size * 0.3))
                .accessibilityHidden(true)
        }
    }
}

/// The summary as the tab's header: its health, title and badges, and a note when a list was cut.
struct PluginSummaryHeader: View {
    let summary: PluginSummary

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 10) {
                HealthDot(health: summary.serviceHealth)
                Text(verbatim: summary.title).font(.headline)
            }
            if !summary.badges.isEmpty {
                ChipFlow {
                    ForEach(Array(summary.badges.enumerated()), id: \.offset) { _, badge in
                        Tag(badge.text, color: pluginToneColor(badge.tone))
                    }
                }
            }
            if summary.partial {
                Text("Only the first objects were read: the counts are a lower bound.")
                    .font(.caption).foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 4)
    }
}

/// A row of a plugin card: its health dot (none when the row has neither status nor health),
/// title, subtitle, status and trailing text; it opens on its detail sections when it has any.
/// `extra` adds a native control under them.
struct PluginRowView: View {
    let row: PluginRow
    /// False when the namespace group's title already says the subtitle.
    let showSubtitle: Bool
    @Binding var expanded: Bool
    /// The kind of an object a relation opens, from the plugin catalog.
    let kindOf: (PluginRef) -> String
    var extra: AnyView?

    var body: some View {
        if row.hasDetail || extra != nil {
            // A DisclosureGroup: VoiceOver says whether it is expanded.
            DisclosureGroup(isExpanded: $expanded) {
                ForEach(Array(row.detail.enumerated()), id: \.offset) { _, section in
                    PluginSectionView(section: section, kindOf: kindOf)
                }
                if let extra { extra }
            } label: {
                label
            }
        } else {
            label
        }
    }

    private var label: some View {
        HStack(alignment: .top, spacing: 12) {
            if row.showsDot {
                HealthDot(health: row.serviceHealth).padding(.top, 5)
            }
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: row.title).font(.subheadline.monospaced()).lineLimit(2)
                let line = row.subtitleLine(showSubtitle: showSubtitle)
                if !line.isEmpty {
                    Text(verbatim: line)
                        .font(.caption)
                        .foregroundStyle(row.serviceHealth.needsAttention ? row.serviceHealth.color : .secondary)
                        .lineLimit(2)
                }
            }
            Spacer(minLength: 8)
            VStack(alignment: .trailing, spacing: 2) {
                if let status = row.status, !status.text.isEmpty {
                    Tag(status.text, color: pluginToneColor(status.tone))
                }
                if !row.trailing.isEmpty {
                    Text(verbatim: row.trailing).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                }
            }
        }
    }
}

/// One detail section; a section this build cannot read draws nothing.
struct PluginSectionView: View {
    let section: PluginSection
    let kindOf: (PluginRef) -> String

    var body: some View {
        switch section {
        case .keyValues(let items):
            ForEach(Array(items.enumerated()), id: \.offset) { _, item in
                PluginKeyValue(item: item)
            }
        case .badges(let items):
            ChipFlow {
                ForEach(Array(items.enumerated()), id: \.offset) { _, item in
                    Tag(item.badgeText, color: pluginToneColor(item.tone))
                }
            }
        case .gauge(let gauge):
            PluginGaugeView(gauge: gauge)
        case .conditions(let items):
            ForEach(Array(items.enumerated()), id: \.offset) { _, condition in
                PluginConditionRow(condition: condition)
            }
        case .relations(let items):
            ForEach(Array(items.enumerated()), id: \.offset) { _, item in
                PluginRelationRow(item: item, kindOf: kindOf)
            }
        case .table(let table):
            ForEach(Array(table.rows.enumerated()), id: \.offset) { _, cells in
                ChipFlow {
                    ForEach(Array(cells.enumerated()), id: \.offset) { index, cell in
                        PluginTableCell(label: table.column(index), value: cell)
                    }
                }
            }
        case .text(let text):
            Text(verbatim: text.text)
                .font(text.mono ? .caption.monospaced() : .callout)
                .foregroundStyle(text.tone.isEmpty ? Color.primary : pluginToneColor(text.tone))
                .textSelection(.enabled)
        case .actions(let items):
            ForEach(Array(items.enumerated()), id: \.offset) { _, action in
                PluginActionButton(action: action)
            }
        case .unknown:
            EmptyView()
        }
    }
}

private struct PluginKeyValue: View {
    let item: PluginItem

    var body: some View {
        LabeledContent {
            Text(verbatim: item.value.isEmpty ? "–" : item.value)
                .font(item.mono ? .callout.monospaced() : .callout)
                .foregroundStyle(item.tone.isEmpty ? Color.secondary : pluginToneColor(item.tone))
                .textSelection(.enabled)
        } label: {
            Text(verbatim: item.label)
        }
    }
}

/// A bar with its label and text ("0 B / 512 MiB"); the bare text when there is no maximum.
private struct PluginGaugeView: View {
    let gauge: PluginGauge

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(verbatim: gauge.label).foregroundStyle(.secondary)
                Spacer()
                if !gauge.text.isEmpty {
                    Text(verbatim: gauge.text).font(.caption.monospaced())
                }
            }
            if let fraction = gauge.fraction {
                ProgressView(value: fraction)
                    .tint(pluginToneColor(gauge.tone.isEmpty ? "ok" : gauge.tone))
                    .accessibilityHidden(true)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

/// A condition, read like the object summary's: its type tinted by tone, status and reason, when
/// it changed, its message.
private struct PluginConditionRow: View {
    let condition: PluginCondition

    var body: some View {
        let now = Int64(Date().timeIntervalSince1970 * 1000)
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 6) {
                Tag(condition.type, color: pluginToneColor(condition.tone))
                Text(verbatim: [condition.status, condition.reason].filter { !$0.isEmpty }.joined(separator: " · "))
                    .font(.caption).lineLimit(1)
                Spacer()
                if condition.lastTransition > 0 {
                    Text(verbatim: CheckupText.kubeEventsAgo(checkupAge(condition.lastTransition, now: now)))
                        .font(.caption).foregroundStyle(.secondary)
                }
            }
            if !condition.message.isEmpty {
                Text(verbatim: condition.message).font(.caption).lineLimit(6)
            }
        }
        .padding(.vertical, 2)
    }
}

/// A related object: opens its object screen, in the active context.
private struct PluginRelationRow: View {
    let item: PluginItem
    let kindOf: (PluginRef) -> String

    var body: some View {
        if let ref = item.ref {
            NavigationLink {
                KubeObjectView(resource: ref.apiResource(kind: kindOf(ref)), namespace: ref.namespace, name: ref.name)
            } label: {
                content
            }
        } else {
            content
        }
    }

    private var content: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(verbatim: item.label).font(.subheadline)
            if !item.target.isEmpty {
                Text(verbatim: item.target).font(.caption.monospaced()).foregroundStyle(.secondary).lineLimit(1)
            }
        }
    }
}

/// A table cell as the resource list draws a field: its column, then its value.
private struct PluginTableCell: View {
    let label: String
    let value: String

    var body: some View {
        HStack(spacing: 4) {
            if !label.isEmpty {
                Text(verbatim: label).font(.caption).foregroundStyle(.secondary)
            }
            Text(verbatim: value.isEmpty ? "–" : value).font(.caption.monospaced())
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 3)
        .background(Color(.tertiarySystemFill), in: Capsule())
        .accessibilityElement(children: .combine)
    }
}

/// A plugin's button. Running a plugin action needs the actions step of the Go core
/// (KubePluginAction), not in this build: every button stays off, with the plugin's reason
/// when it gives one, else why.
private struct PluginActionButton: View {
    let action: PluginActionItem

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Button {} label: { Text(verbatim: action.label) }
                .buttonStyle(.bordered)
                .disabled(true)
            Text(note).font(.caption).foregroundStyle(.secondary)
        }
    }

    private var note: String {
        switch action.note {
        case .reason(let reason): reason
        case .notAllowed: String(localized: "Your credentials may not do this.")
        case .unavailable: String(localized: "Not available in this version yet.")
        }
    }
}
