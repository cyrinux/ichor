import IchorCore
import SwiftUI

/// The iPad's root at regular width: a sidebar with "All clusters" (the fleet view), the
/// clusters and the active cluster's sections, beside the app's NavigationStack in the detail
/// column (`detail`). The split view sits at the root and the stack inside it, the supported
/// shape (a split view inside the stack breaks Back, see AdaptiveListDetail.swift). `path` stays MainNavigation's, so share
/// links, notifications and quick actions route as on the iPhone.
struct SidebarNavigation<Detail: View>: View {
    @Binding var path: [Route]
    let detail: Detail

    @Environment(AppModel.self) private var model
    @State private var visibility: NavigationSplitViewVisibility = .all

    init(path: Binding<[Route]>, @ViewBuilder detail: () -> Detail) {
        _path = path
        self.detail = detail()
    }

    var body: some View {
        NavigationSplitView(columnVisibility: $visibility) {
            List {
                Section {
                    Button {
                        path = [.fleet]
                    } label: {
                        Label("All clusters", systemImage: "rectangle.stack")
                    }
                    .accessibilityAddTraits(path.last == .fleet ? .isSelected : [])
                }
                Section("Clusters") {
                    ForEach(model.summary?.contexts ?? []) { context in
                        clusterRow(context)
                    }
                }
                Section {
                    ForEach(SidebarSection.shown(kubeReads: model.allows(.workloads))) { section in
                        Button {
                            path = section.route(cluster: model.activeContext).map { [$0] } ?? []
                        } label: {
                            Label {
                                section.title
                            } icon: {
                                Image(systemName: section.systemImage)
                            }
                        }
                    }
                } header: {
                    Text(verbatim: model.activeLabel)
                }
            }
            .listStyle(.sidebar)
            .navigationTitle(Text(verbatim: "Ichor"))
        } detail: {
            detail
        }
        .navigationSplitViewStyle(.balanced)
    }

    private func clusterRow(_ context: ContextSummary) -> some View {
        let active = context.name == model.activeContext
        return Button {
            guard !active else { return }
            // The screens open belong to the cluster left behind.
            path = []
            model.activeContext = context.name
        } label: {
            HStack {
                Circle().fill(clusterAccent(model.seed(of: context))).frame(width: 10, height: 10)
                Text(verbatim: model.labels.of(context))
                Spacer()
                if active { Image(systemName: "checkmark").foregroundStyle(.tint) }
            }
        }
        .accessibilityAddTraits(active ? .isSelected : [])
    }
}

/// The active cluster's sections in the sidebar: each opens its screen as the only one in the
/// detail column (Overview pops to the home).
enum SidebarSection: String, CaseIterable, Identifiable {
    case overview, alerts, logSearch, kubernetes, argoCD, flux, dataServices, activity, search, settings

    var id: String { rawValue }

    /// The sections shown: those reading the Kubernetes API only with a role that does.
    static func shown(kubeReads: Bool) -> [SidebarSection] {
        allCases.filter { kubeReads || !$0.readsKube }
    }

    private var readsKube: Bool {
        switch self {
        case .logSearch, .kubernetes, .argoCD, .flux, .dataServices: true
        default: false
        }
    }

    /// Product names stay as they are; the others reuse the app's translated titles.
    var title: Text {
        switch self {
        case .overview: Text("Overview")
        case .alerts: Text("Alerts")
        case .logSearch: Text("Log search")
        case .kubernetes: Text(verbatim: "Kubernetes")
        case .argoCD: Text(verbatim: "Argo CD")
        case .flux: Text(verbatim: "Flux")
        case .dataServices: Text("Data services")
        case .activity: Text("Activity")
        case .search: Text("Search clusters")
        case .settings: Text("Settings")
        }
    }

    var systemImage: String {
        switch self {
        case .overview: "square.grid.2x2"
        case .alerts: "bell"
        case .logSearch: "text.magnifyingglass"
        case .kubernetes: "shippingbox"
        case .argoCD: "arrow.triangle.2.circlepath"
        case .flux: "arrow.triangle.branch"
        case .dataServices: "cylinder.split.1x2"
        case .activity: "list.bullet.rectangle"
        case .search: "magnifyingglass"
        case .settings: "gearshape"
        }
    }

    /// The screen it opens; nil for the overview (the home, under the stack).
    func route(cluster: String) -> Route? {
        switch self {
        case .overview: nil
        case .alerts: .alerts
        case .logSearch: .logSearch()
        case .kubernetes: .workloads
        case .argoCD: .argoCD(downNodes: [])
        case .flux: .flux(downNodes: [])
        case .dataServices: .dataServices(hints: "", downNodes: [])
        case .activity: .activity(cluster: cluster)
        case .search: .search
        case .settings: .settings
        }
    }
}
