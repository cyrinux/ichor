import SwiftUI
import IchorCore

/// What the two homes (the Talos overview and the Kubernetes home) wear alike: the cluster's
/// name as the title with its menu (switch cluster, manage clusters, the share link), the
/// cluster bar under the navigation bar when there are several clusters, the screenshot-mode
/// mark, and the toolbar as arranged: the AI diagnosis when on, the bar's icons, the rest
/// behind ⋯ with the home's editor. Buttons with labels, not NavigationLinks with images: on
/// a phone the ones that don't fit fold into the system's "…" menu, where a link does nothing
/// and an image shows no title.
struct HomeChrome<Action: BarAction & BarActionLook>: ViewModifier {
    @Binding var path: [Route]
    /// The bar's icons, then its menu, as arranged and narrowed to what the role offers.
    let icons: [Action]
    let menu: [Action]
    /// Whether the action can be used now (a cluster-wide screen no node has is greyed).
    let enabled: (Action) -> Bool
    /// "Customize overview" / "Customize home".
    let customizeTitle: LocalizedStringKey
    @Binding var customizing: Bool
    let open: (Action) -> Void

    @Environment(AppModel.self) private var model
    @Environment(AISettings.self) private var ai

    func body(content: Content) -> some View {
        content
            .navigationTitle(model.activeLabel)
            // Shown by the title once it is inline (scrolled); the bar below is always there.
            .toolbarTitleMenu {
                // A submenu: with many clusters, the actions below stay in reach without a scroll.
                if (model.summary?.contexts.count ?? 0) > 1 {
                    Menu {
                        ForEach(model.summary?.contexts ?? []) { context in
                            Button { model.activeContext = context.name } label: {
                                if context.name == model.activeContext {
                                    Label(model.labels.of(context), systemImage: "checkmark")
                                } else {
                                    Text(model.labels.of(context))
                                }
                            }
                        }
                    } label: {
                        Label("Switch cluster", systemImage: "arrow.left.arrow.right")
                    }
                }
                Button { path.append(.clusters) } label: { Label("Manage clusters…", systemImage: "square.stack.3d.up") }
                ShareLinkButton(target: .screen(.cluster))
            }
            .safeAreaInset(edge: .top, spacing: 0) {
                if (model.summary?.contexts.count ?? 0) > 1 {
                    ClusterBar { path.append(.clusters) }
                }
            }
            .toolbar {
                if model.privacyMask {
                    ToolbarItem(placement: .topBarLeading) {
                        // A small icon rather than a label, so it stays out of the way in screenshots.
                        Image(systemName: "eye.slash")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                            .accessibilityLabel(Text("Screenshot mode"))
                    }
                }
                ToolbarItemGroup(placement: .primaryAction) {
                    // Optional: only once turned on in the settings.
                    if ai.enabled {
                        Button { path.append(Route.diagnosis(note: "")) } label: { Label("AI diagnosis", systemImage: "sparkles") }
                    }
                    // As arranged: the bar's icons, the rest behind ⋯ (with the arrangement itself).
                    ForEach(icons) { action in
                        Button { open(action) } label: {
                            Label { action.title } icon: { Image(systemName: action.systemImage) }
                        }
                        .disabled(!enabled(action))
                    }
                    Menu {
                        ForEach(menu) { action in
                            Button { open(action) } label: {
                                Label { action.title } icon: { Image(systemName: action.systemImage) }
                            }
                            .disabled(!enabled(action))
                        }
                        if !menu.isEmpty { Divider() }
                        Button { customizing = true } label: { Label(customizeTitle, systemImage: "pencil") }
                    } label: {
                        Image(systemName: "ellipsis.circle")
                    }
                    .accessibilityLabel(Text("More"))
                }
            }
    }
}

extension View {
    /// The homes' shared title, menus and toolbar (`HomeChrome`); `open` takes an action of the
    /// bar to its screen.
    func homeChrome<Action: BarAction & BarActionLook>(
        path: Binding<[Route]>, icons: [Action], menu: [Action], enabled: @escaping (Action) -> Bool = { _ in true },
        customizeTitle: LocalizedStringKey, customizing: Binding<Bool>, open: @escaping (Action) -> Void
    ) -> some View {
        modifier(HomeChrome(path: path, icons: icons, menu: menu, enabled: enabled, customizeTitle: customizeTitle,
                            customizing: customizing, open: open))
    }
}

/// A home with every section hidden: one row that opens its editor.
struct AllHiddenRow: View {
    let customize: () -> Void

    var body: some View {
        Section {
            Button(action: customize) {
                Text("Every card is hidden. Tap to choose the ones to show.").foregroundStyle(.secondary)
            }
        }
    }
}
