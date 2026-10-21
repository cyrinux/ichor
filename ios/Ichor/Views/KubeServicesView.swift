import SwiftUI
import IchorCore

/// The Services of the Kubernetes screens' namespace scope, problems first: load balancers
/// without an address (with a hint on the pool to check), then Services with no ready
/// endpoint. Each shows its type, cluster IP and ports, address, ready endpoints and the
/// URLs of the routes exposing it. A Service opens its summary; a route opens its URL, and its
/// own summary behind the info button beside it.
struct KubeServicesView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.openURL) private var openURL
    @State private var query = ""

    var body: some View {
        KubeScopedList(
            title: Text("Services"),
            prompt: Text("Search services, addresses or routes"),
            query: $query,
            fetch: { client, scope in try await client.services(namespace: scope.namespace) },
            namespaces: { value in value.services.map(\.namespace) }
        ) { services, scope, reload in
            list(services, showNamespace: scope.namespace == nil, reload: reload)
        }
    }

    private func list(_ services: KubeServices, showNamespace: Bool, reload: @escaping () async -> Void) -> some View {
        let shown = filterServices(services.services, query: query)
        return List {
            if services.partialAccess {
                Text("These credentials may not read endpoints or routes: some details are missing.")
                    .font(.footnote).foregroundStyle(.secondary)
            }
            if services.anyPending {
                NavigationLink { KubeLoadBalancingView() } label: {
                    VStack(alignment: .leading, spacing: 4) {
                        lbHint(LBController(rawValue: services.lbController))
                            .font(.footnote).foregroundStyle(.secondary)
                        Text(verbatim: LoadBalancingText.open).font(.footnote)
                    }
                }
            }
            ForEach(shown) { service in
                NavigationLink {
                    KubeObjectView(resource: KubeAPIResource(resource: "services", kind: "Service"),
                                   namespace: service.namespace, name: service.name)
                } label: {
                    ServiceRowView(service: service, showNamespace: showNamespace)
                }
                ForEach(service.routes) { route in
                    HStack(spacing: 4) {
                        if let url = URL(string: route.url) {
                            Button { openURL(url) } label: {
                                Label { Text(verbatim: route.label) } icon: { Image(systemName: "arrow.up.right.square") }
                                    .font(.footnote)
                            }
                            .buttonStyle(.plain)
                        }
                        RouteCertBadge(route: route)
                        RouteSummaryLink(route: route)
                    }
                }
            }
        }
        .emptyOverlay(shown.isEmpty, query: query) {
            ContentUnavailableView("No services.", systemImage: "network")
        }
        .refreshable { await reload() }
        .themedBackground()
    }

    /// What to check when a load balancer has no address, by the controller found.
    private func lbHint(_ controller: LBController?) -> Text {
        switch controller {
        case .metallb:
            Text("A load balancer has no address: check MetalLB’s IPAddressPool, and an L2Advertisement or BGPAdvertisement that announces it.")
        case .cilium:
            Text("A load balancer has no address: check the CiliumLoadBalancerIPPool (addresses left, its service selector) and that Cilium’s LB IPAM is on.")
        case .kubeVip:
            Text(verbatim: LoadBalancingText.servicesHintKubeVip)
        case nil:
            Text("A load balancer has no address and no controller was found to give one: install MetalLB, turn on Cilium’s LB IPAM, or use a cloud’s load balancer.")
        }
    }
}

/// A Service: name and type (coloured by level), cluster IP and ports, its address or "No
/// address", the ready endpoints.
private struct ServiceRowView: View {
    let service: ServiceRow
    let showNamespace: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline) {
                Text(verbatim: showNamespace ? service.id : service.name)
                    .font(.callout.monospaced()).lineLimit(1).truncationMode(.middle)
                Spacer()
                Text(verbatim: service.type).font(.caption).foregroundStyle(levelColor)
            }
            let inside = [service.headless ? String(localized: "headless") : service.clusterIP,
                          service.externalName.isEmpty ? "" : "→ \(service.externalName)",
                          service.ports.joined(separator: ", ")].filter { !$0.isEmpty }
            if !inside.isEmpty {
                Text(verbatim: inside.joined(separator: " · ")).font(.caption).foregroundStyle(.secondary).lineLimit(1)
            }
            if service.pending {
                Text("No address").font(.caption).foregroundStyle(levelColor)
            } else if !service.addresses.isEmpty {
                Text(verbatim: service.addresses.joined(separator: ", ")).font(.caption.monospaced())
            }
            if let ready = service.readyText {
                Text("Ready endpoints: \(ready)").font(.caption).foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .combine)
    }

    private var levelColor: Color {
        switch service.level {
        case .critical: .statusBad
        case .warning: .statusWarn
        case .ok: .secondary
        }
    }
}
