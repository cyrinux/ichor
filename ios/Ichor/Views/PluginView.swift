import SwiftUI
import IchorCore

/// A plugin as a screen of its own: one of another category than Data services, opened from
/// the Kubernetes home's tools. Starts over on another cluster or screenshot mode switch.
struct PluginView: View {
    let pluginID: String

    @Environment(AppModel.self) private var model
    @State private var name: String?

    var body: some View {
        PluginList(
            pluginID: pluginID,
            reload: PluginReload(scope: "\(model.activeContext)#\(model.dataGeneration)", bump: 0),
            onName: { name = $0 }
        )
        // A new API address (set on the Kubernetes screen): read again through it.
        .id(model.client?.kubeServer)
        .navigationTitle(name ?? String(localized: "Plugin"))
        .navigationBarTitleDisplayMode(.inline)
    }
}

/// Where a plugin's native screen (`screen: native:<route>`) leads: only the screens of the
/// allow-list (PluginNativeScreen), the same ones the app opens from its own rows.
struct PluginNativeDestination: View {
    let screen: PluginNativeScreen

    var body: some View {
        switch screen {
        case .argoCD: ArgoCDView(downNodes: [])
        case .flux: FluxView(downNodes: [])
        case .argoRollouts: ArgoRolloutsView()
        case .policies: PoliciesView()
        case .netpol: NetPoliciesView()
        case .autoscaling: AutoscalingView()
        case .vms: VirtualMachinesView()
        case .crossplane: CrossplaneView()
        case .tailscale: TailscaleView()
        case .clusterLifecycle: ClusterLifecycleView()
        case .loadBalancing: KubeLoadBalancingView()
        case .storage: KubeStorageView()
        case .services: KubeServicesView()
        case .jobs: KubeJobsView()
        case .helm: HelmReleasesView()
        case .dataService(let kind): DataServicesView(hints: "", downNodes: [], selected: kind)
        }
    }
}

/// A plugin's tile as a link: its native screen, else its Data services tab, else its own screen.
struct PluginTileLink<Label: View>: View {
    let item: PluginDetectItem
    var hints = ""
    var downNodes: Set<String> = []
    @ViewBuilder let label: () -> Label

    var body: some View {
        if let native = item.nativeScreen {
            NavigationLink { PluginNativeDestination(screen: native) } label: { label() }
        } else if item.category == pluginCategoryDataServices {
            NavigationLink(value: Route.dataServicesPlugin(hints: hints, downNodes: downNodes, plugin: item.id)) { label() }
        } else {
            NavigationLink(value: Route.plugin(id: item.id)) { label() }
        }
    }
}
