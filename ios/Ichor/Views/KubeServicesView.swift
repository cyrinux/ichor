import SwiftUI
import IchorCore

/// The Services of the Kubernetes screens' namespace scope, problems first: load balancers
/// without an address (with a hint on the pool to check), then Services with no ready
/// endpoint. Each shows its type, cluster IP and ports, address, ready endpoints and the
/// URLs of the routes exposing it. A Service opens its summary; a route opens its URL.
struct KubeServicesView: View {
    @Environment(AppModel.self) private var model
    @State private var scope = KubeBrowserScope()
    @State private var state: LoadState<KubeServices> = .loading
    @State private var query = ""

    /// What decides the list: the scope, and the cluster, API address and screenshot mode.
    private struct Trigger: Hashable {
        let scope: KubeScope
        let ready: Bool
        let source: String
    }

    var body: some View {
        let control = scope.control(model: model)
        VStack(spacing: 0) {
            KubeScopeBar(control: control, loaded: loadedNamespaces)
                .background(.bar)
            Divider()
            if !control.ready {
                ContentUnavailableView {
                    Label("Namespace", systemImage: "square.dashed")
                } description: {
                    Text("Type the namespace to list above.")
                }
                .frame(maxHeight: .infinity)
            } else {
                LoadStateView(state: state, retry: { await load(control.scope) }) { services in
                    list(services, showNamespace: control.scope.namespace == nil, scope: control.scope)
                }
            }
        }
        .searchable(text: $query, prompt: Text("Search services, addresses or routes"))
        .autocorrectionDisabled()
        .textInputAutocapitalization(.never)
        .navigationTitle(Text("Services"))
        .navigationBarTitleDisplayMode(.inline)
        .task(id: kubeNamespacesKey(model)) { await scope.loadNamespaces(model: model) }
        .task(id: Trigger(scope: control.scope, ready: control.ready, source: kubeNamespacesKey(model))) {
            if control.ready { await load(control.scope) }
        }
    }

    private func list(_ services: KubeServices, showNamespace: Bool, scope: KubeScope) -> some View {
        let shown = filterServices(services.services, query: query)
        return List {
            if services.partialAccess {
                Text("These credentials may not read endpoints or routes: some details are missing.")
                    .font(.footnote).foregroundStyle(.secondary)
            }
            if services.anyPending {
                lbHint(LBController(rawValue: services.lbController))
                    .font(.footnote).foregroundStyle(.secondary)
            }
            ForEach(shown) { service in
                NavigationLink {
                    KubeObjectView(resource: KubeAPIResource(resource: "services", kind: "Service"),
                                   namespace: service.namespace, name: service.name)
                } label: {
                    ServiceRowView(service: service, showNamespace: showNamespace)
                }
                ForEach(service.routes) { route in
                    if let url = URL(string: route.url) {
                        Link(destination: url) {
                            Label { Text(verbatim: route.label) } icon: { Image(systemName: "arrow.up.right.square") }
                                .font(.footnote)
                        }
                    }
                }
            }
        }
        .emptyOverlay(shown.isEmpty, query: query) {
            ContentUnavailableView("No services.", systemImage: "network")
        }
        .refreshable { await load(scope) }
        .themedBackground()
    }

    /// What to check when a load balancer has no address, by the controller found.
    private func lbHint(_ controller: LBController?) -> Text {
        switch controller {
        case .metallb:
            Text("A load balancer has no address: check MetalLB’s IPAddressPool, and an L2Advertisement or BGPAdvertisement that announces it.")
        case .cilium:
            Text("A load balancer has no address: check the CiliumLoadBalancerIPPool (addresses left, its service selector) and that Cilium’s LB IPAM is on.")
        case nil:
            Text("A load balancer has no address and no controller was found to give one: install MetalLB, turn on Cilium’s LB IPAM, or use a cloud’s load balancer.")
        }
    }

    private var loadedNamespaces: [String] {
        guard case .loaded(let services, _, _) = state else { return [] }
        return Array(Set(services.services.map(\.namespace))).sorted()
    }

    private func load(_ scope: KubeScope) async {
        guard let client = model.client else { return }
        let key = kubeNamespacesKey(model)
        let fetched: LoadState<KubeServices> = await .from { try await client.services(namespace: scope.namespace) }
        guard key == kubeNamespacesKey(model), !Task.isCancelled else { return }
        state = state.refreshed(with: fetched)
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
