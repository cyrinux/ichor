import SwiftUI
import IchorCore

extension AppCategory {
    var label: String {
        switch self {
        case .system: String(localized: "System")
        case .networking: String(localized: "Networking")
        case .storage: String(localized: "Storage")
        case .observability: String(localized: "Observability")
        case .security: String(localized: "Security")
        case .database: String(localized: "Databases")
        case .messaging: String(localized: "Messaging")
        case .devops: String(localized: "DevOps")
        case .media: String(localized: "Media")
        case .home: String(localized: "Home automation")
        case .productivity: String(localized: "Productivity")
        case .ai: String(localized: "AI")
        case .web: String(localized: "Web")
        case .other: String(localized: "Other")
        }
    }
}

extension ClusterInventory {
    /// "12 apps · 31 containers · 5 nodes", or "4 of 5 nodes" when some did not answer.
    var localizedSummary: String {
        let nodesPart = answered < nodes
            ? String(localized: "\(answered) of \(nodes) nodes")
            : String(localized: "\(answered) nodes")
        return [String(localized: "\(apps.count) apps"), String(localized: "\(containers) containers"), nodesPart]
            .joined(separator: " · ")
    }
}

/// The amber of everything that needs a look, like the app's other warnings.
let attentionColor = Color.orange

/// An app in the grid: icon, name, and its version or what needs a look.
struct AppTile: View {
    let app: InventoryApp
    /// Its Argo CD Application is broken or drifting.
    var argoBadge = false

    var body: some View {
        VStack(spacing: 6) {
            AppIconView(app: app)
                .overlay(alignment: .topTrailing) {
                    if app.needsAttention { AttentionDot().offset(x: 3, y: -3) }
                }
                .overlay(alignment: .bottomTrailing) {
                    if argoBadge { ArgoTileBadge().offset(x: 4, y: 4) }
                }
            Text(verbatim: app.name)
                .font(.caption.weight(.semibold))
                .lineLimit(1)
                .truncationMode(.tail)
            versionLine
                .font(.caption2.monospaced())
                .lineLimit(1)
                .truncationMode(.middle)
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 12)
        .padding(.horizontal, 6)
        .background(Color(.secondarySystemGroupedBackground), in: RoundedRectangle(cornerRadius: 16, style: .continuous))
        .contentShape(RoundedRectangle(cornerRadius: 16, style: .continuous))
        .accessibilityElement(children: .combine)
    }

    @ViewBuilder
    private var versionLine: some View {
        if app.drift {
            Text("\(app.versionCount) versions").foregroundStyle(attentionColor)
        } else if app.unpinned {
            Text(verbatim: ":latest").foregroundStyle(attentionColor)
        } else {
            Text(verbatim: app.version.isEmpty ? "—" : app.version).foregroundStyle(.secondary)
        }
    }
}

struct AttentionDot: View {
    var size: CGFloat = 12

    var body: some View {
        Circle()
            .fill(attentionColor)
            .frame(width: size, height: size)
            .overlay { Circle().stroke(Color(.secondarySystemGroupedBackground), lineWidth: 2) }
            .accessibilityLabel(Text("Needs a look"))
    }
}

/// A filter chip: its label and count, filled with the accent color when selected.
struct FilterChip: View {
    let label: String
    let count: Int
    let selected: Bool
    var dot: Color?
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 6) {
                if let dot { Circle().fill(dot).frame(width: 7, height: 7).accessibilityHidden(true) }
                Text(verbatim: label)
                Text(verbatim: "\(count)").monospacedDigit().opacity(0.7)
            }
            .font(.subheadline.weight(.medium))
            .padding(.horizontal, 12)
            .padding(.vertical, 7)
            .foregroundStyle(selected ? AnyShapeStyle(Color.white) : AnyShapeStyle(HierarchicalShapeStyle.primary))
            .background(selected ? AnyShapeStyle(TintShapeStyle.tint) : AnyShapeStyle(Color(.tertiarySystemFill)), in: Capsule())
            // A 44 pt touch target around the smaller capsule.
            .frame(minWidth: 44, minHeight: 44)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}

/// A small rounded label: a version (monospaced) or an amber warning.
struct InfoChip: View {
    let text: String
    var monospaced = false
    var color: Color?

    var body: some View {
        Text(verbatim: text)
            .font(monospaced ? .caption.monospaced() : .caption.weight(.medium))
            .padding(.horizontal, 10)
            .padding(.vertical, 4)
            .foregroundStyle(color.map { AnyShapeStyle($0) } ?? AnyShapeStyle(HierarchicalShapeStyle.primary))
            .background(color.map { AnyShapeStyle($0.opacity(0.15)) } ?? AnyShapeStyle(Color(.tertiarySystemFill)), in: Capsule())
    }
}
