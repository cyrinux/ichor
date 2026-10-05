import SwiftUI
import IchorCore

// How Argo CD states look everywhere in the app: Argo-style glyphs (heart, broken heart,
// arrows), colors and labels, and the small pieces the Argo CD screens share.

extension ArgoHealth {
    var label: String {
        switch self {
        case .healthy: String(localized: "Healthy")
        case .progressing: String(localized: "Progressing")
        case .degraded: String(localized: "Degraded")
        case .suspended: String(localized: "Suspended")
        case .missing: String(localized: "Missing")
        case .unknown: String(localized: "Unknown")
        }
    }

    var color: Color {
        switch self {
        case .healthy: .green
        case .progressing: .blue
        case .degraded: .red
        case .missing: attentionColor
        case .suspended, .unknown: .secondary
        }
    }

    var symbol: String {
        switch self {
        case .healthy: "heart.fill"
        case .progressing: "arrow.triangle.2.circlepath"
        case .degraded: "heart.slash.fill"
        case .suspended: "pause.circle.fill"
        case .missing: "questionmark.diamond.fill"
        case .unknown: "questionmark.circle"
        }
    }
}

extension ArgoSyncState {
    var label: String {
        switch self {
        case .synced: String(localized: "Synced")
        case .outOfSync: String(localized: "Out of sync")
        case .unknown: String(localized: "Unknown")
        }
    }

    var color: Color {
        switch self {
        case .synced: .green
        case .outOfSync: attentionColor
        case .unknown: .secondary
        }
    }

    var symbol: String {
        switch self {
        case .synced: "checkmark.circle.fill"
        case .outOfSync: "arrow.up.circle.fill"
        case .unknown: "questionmark.circle"
        }
    }
}

extension ArgoPhase {
    var label: String {
        switch self {
        case .running: String(localized: "Syncing")
        case .terminating: String(localized: "Terminating")
        case .succeeded: String(localized: "Succeeded")
        case .failed: String(localized: "Failed")
        case .error: String(localized: "Error")
        case .unknown: String(localized: "Unknown")
        }
    }

    var color: Color {
        switch self {
        case .running, .terminating: .blue
        case .succeeded: .green
        case .failed, .error: .red
        case .unknown: .secondary
        }
    }
}

extension ArgoFilter {
    var label: String {
        switch self {
        case .all: String(localized: "All")
        case .degraded: String(localized: "Degraded")
        case .outOfSync: String(localized: "Out of sync")
        case .progressing: String(localized: "Progressing")
        case .syncing: String(localized: "Syncing")
        case .autoSyncOff: String(localized: "Auto-sync off")
        case .failed: String(localized: "Failed")
        case .frozen: String(localized: "Frozen")
        }
    }

    var dot: Color? {
        switch self {
        case .all, .autoSyncOff: nil
        case .degraded, .failed: .red
        case .outOfSync: attentionColor
        case .progressing, .syncing, .frozen: .blue
        }
    }
}

extension ArgoGrouping {
    var label: String {
        switch self {
        case .none: String(localized: "No grouping")
        case .project: String(localized: "Project")
        case .appSet: String(localized: "ApplicationSet")
        case .namespace: String(localized: "Namespace")
        }
    }
}

extension ArgoCause {
    /// A pod or its node, rather than a message of Argo CD's.
    var pointsAtPod: Bool {
        switch self {
        case .nodeDown, .pod: true
        case .message: false
        }
    }

    /// One line: "pod worker-7f9c on worker-2, which is not ready".
    var text: String {
        switch self {
        case .nodeDown(let node, let pod): String(localized: "pod \(pod) on \(node), which is not ready")
        case .pod(let name, let status, let node):
            [String(localized: "pod \(name): \(status)"), node.isEmpty ? nil : String(localized: "on \(node)")]
                .compactMap { $0 }.joined(separator: " ")
        case .message(let message): message
        }
    }
}

extension ArgoApp {
    /// "Managed by ApplicationSet infra: change it in Git", nil when the app is its own.
    var ownerNotice: String? {
        owner.map { String(localized: "Managed by \($0.kind) \($0.name): change it in Git") }
    }
}

/// The health and sync glyphs side by side, colored, with an accessible label.
struct ArgoGlyphs: View {
    let health: ArgoHealth
    let sync: ArgoSyncState
    var running = false
    var font: Font = .subheadline

    var body: some View {
        HStack(spacing: 4) {
            Image(systemName: health.symbol)
                .foregroundStyle(health.color)
                .symbolEffect(.pulse, isActive: health == .progressing)
            Image(systemName: running ? "arrow.triangle.2.circlepath.circle.fill" : sync.symbol)
                .foregroundStyle(running ? Color.blue : sync.color)
                .symbolEffect(.pulse, isActive: running)
        }
        .font(font)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text(verbatim: "\(health.label), \(running ? ArgoPhase.running.label : sync.label)"))
    }
}

/// A capsule with a glyph and a label: the hero's health and sync badges.
struct ArgoBadge: View {
    let label: String
    let symbol: String
    let color: Color

    var body: some View {
        Label { Text(verbatim: label) } icon: { Image(systemName: symbol) }
            .font(.subheadline.weight(.semibold))
            .padding(.horizontal, 12)
            .padding(.vertical, 6)
            .foregroundStyle(color)
            .background(color.opacity(0.15), in: Capsule())
    }
}

/// "⧉ infra": the ApplicationSet (or parent app) that writes the app's spec.
struct ArgoOwnerBadge: View {
    let owner: ArgoOwner

    var body: some View {
        Label { Text(verbatim: owner.name).lineLimit(1) } icon: {
            Image(systemName: owner.kind == "ApplicationSet" ? "square.stack.3d.down.right" : "arrow.up.forward.square")
        }
        .labelStyle(.titleAndIcon)
        .font(.caption2.weight(.medium))
        .padding(.horizontal, 6)
        .padding(.vertical, 2)
        .foregroundStyle(.secondary)
        .background(Color(.tertiarySystemFill), in: Capsule())
        .accessibilityLabel(Text("Managed by \(owner.kind) \(owner.name)"))
    }
}

/// The apps' health as one bar split by state, proportional to their counts.
struct ArgoHealthBar: View {
    let counts: [(health: ArgoHealth, count: Int)]
    var height: CGFloat = 8

    var body: some View {
        SegmentBar(segments: counts.map { (color: $0.health.color, count: $0.count, label: $0.health.label) }, height: height)
    }
}

/// One bar split into colored segments proportional to their counts (Argo CD's and Flux's
/// health bars).
struct SegmentBar: View {
    let segments: [(color: Color, count: Int, label: String)]
    var height: CGFloat = 8

    var body: some View {
        let total = max(segments.reduce(0) { $0 + $1.count }, 1)
        GeometryReader { geo in
            let spacing: CGFloat = 2
            let width = geo.size.width - spacing * CGFloat(max(segments.count - 1, 0))
            HStack(spacing: spacing) {
                ForEach(segments.indices, id: \.self) { i in
                    Rectangle()
                        .fill(segments[i].color)
                        .frame(width: max(width * CGFloat(segments[i].count) / CGFloat(total), 3))
                }
            }
        }
        .frame(height: height)
        .clipShape(Capsule())
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text(verbatim: segments.map { "\($0.count) \($0.label)" }.joined(separator: ", ")))
    }
}

/// "wave 1 · 5/9": where a running sync stands.
func argoProgressText(_ op: ArgoOperation) -> String {
    [op.waves.count > 1 ? String(localized: "wave \(op.wave)") : nil, op.total > 0 ? "\(op.done)/\(op.total)" : nil]
        .compactMap { $0 }.joined(separator: " · ")
}
