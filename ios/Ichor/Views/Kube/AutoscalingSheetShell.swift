import SwiftUI
import IchorCore

/// The frame of an autoscaling detail sheet: `content`'s sections in a list, in a navigation
/// stack with Done, at the medium and large detents.
struct AutoscalingSheetShell<Content: View>: View {
    @ViewBuilder let content: () -> Content
    @Environment(\.dismiss) private var dismiss

    // Explicit: the private environment value makes the memberwise init private.
    init(@ViewBuilder content: @escaping () -> Content) {
        self.content = content
    }

    var body: some View {
        NavigationStack {
            List { content() }
                .themedBackground()
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } }
                }
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
    }
}

/// An autoscaling sheet's head: the name (selectable, monospaced) with its badge, the
/// namespace · target line, then what each kind adds.
struct AutoscalingSheetHeader<Badge: View, Extra: View>: View {
    let name: String
    /// The namespace · target line; nil for a cluster-wide object (a NodePool).
    let line: String?
    let badge: Badge
    let extra: Extra

    init(name: String, line: String?, @ViewBuilder badge: () -> Badge, @ViewBuilder extra: () -> Extra) {
        self.name = name
        self.line = line
        self.badge = badge()
        self.extra = extra()
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Text(verbatim: name).font(.headline.monospaced()).textSelection(.enabled)
                Spacer()
                badge
            }
            if let line {
                Text(verbatim: line).font(.subheadline).foregroundStyle(.secondary)
            }
            extra
        }
    }
}

extension AutoscalingSheetHeader where Badge == AutoscaleStateBadge {
    init(name: String, line: String?, state: AutoscaleState, @ViewBuilder extra: () -> Extra) {
        self.init(name: name, line: line, badge: { AutoscaleStateBadge(state: state) }, extra: extra)
    }
}
