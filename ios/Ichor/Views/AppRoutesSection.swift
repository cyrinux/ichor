import SwiftUI
import IchorCore

/// The URLs an app is served at, from the Ingresses and HTTPRoutes pointing to the Services
/// that select its pods (os:admin), each opening in the browser. Nothing at all when it has
/// none (most apps are internal).
struct AppRoutesSection: View {
    let app: InventoryApp

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<[KubeRoute]> = .loading

    var body: some View {
        content
            .task(id: app) {
                state = .loading
                await load()
            }
    }

    @ViewBuilder private var content: some View {
        switch state {
        case .loading:
            Section("Web access") { Text("Finding its addresses…").note() }
        case .failed(let message):
            Section("Web access") { Text("Could not find its addresses: \(message)").note() }
        case .loaded(let routes, _, _):
            if !routes.isEmpty {
                Section("Web access") { ForEach(routes) { AppRouteLine(route: $0) } }
            }
        }
    }

    private func load() async {
        guard let client = model.client else { return }
        let pods = app.routePods
        state = await .from { pods.isEmpty ? [] : try await client.appRoutes(pods: pods) }
    }
}

/// The URL, "Ingress · namespace/name", an open icon and an info button; the row opens the
/// URL, the info button the route's own summary (its Accepted and ResolvedRefs conditions).
private struct AppRouteLine: View {
    let route: KubeRoute

    @Environment(\.openURL) private var openURL

    var body: some View {
        HStack(spacing: 4) {
            if let url = URL(string: route.url) {
                Button { openURL(url) } label: { labels }
                    .buttonStyle(.plain)
            } else {
                labels
            }
            RouteSummaryLink(route: route)
        }
    }

    private var labels: some View {
        HStack(spacing: 10) {
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: route.label)
                    .font(.callout.monospaced())
                    .lineLimit(1)
                    .truncationMode(.middle)
                Text(verbatim: "\(route.kind) · \(route.namespace)/\(route.name)")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                RouteCertBadge(route: route)
            }
            Spacer(minLength: 8)
            Image(systemName: "arrow.up.right.square")
                .accessibilityLabel(Text("Open in browser"))
        }
    }
}

/// An (i) opening the route's own summary, where it was read. Nothing when the route carries
/// no resource (an older answer).
struct RouteSummaryLink: View {
    let route: KubeRoute

    var body: some View {
        if let resource = route.apiResource {
            NavigationLink {
                KubeObjectView(resource: resource, namespace: route.namespace, name: route.name)
            } label: {
                Image(systemName: "info.circle")
                    .foregroundStyle(.secondary)
                    .frame(minWidth: 44, minHeight: 44)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.borderless)
            .accessibilityLabel(Text("Open object summary"))
        }
    }
}
