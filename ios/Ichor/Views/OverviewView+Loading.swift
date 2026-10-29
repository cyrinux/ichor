import SwiftUI
import IchorCore

// The overview's Kubernetes-backed sections (Argo CD, Flux, data services, alerts), loaded once
// the inventory shows what the cluster runs. Split from OverviewView.swift for its length: the
// state they set and loadID are internal for this file.
extension OverviewView {
    /// Only for clusters whose inventory shows Argo CD, and roles that may use the Kubernetes API.
    /// The answer is shared (ArgoCDStore) with the Argo CD screens and the Apps grid's badges.
    func loadArgo(with client: TalosClient, id: String, inventory apps: ClusterInventory) async {
        guard model.allows(.workloads), argoCDHinted(apps) else {
            argo = nil
            return
        }
        if argo == nil { argo = .loading }
        let key = model.argoKey
        let cluster = model.activeSummary
        let loaded: LoadState<ArgoStatus> = await .from {
            try await ArgoCDStore.shared.load(with: client, key: key, cluster: cluster)
        }
        guard id == loadID else { return }
        argo = (argo ?? .loading).refreshed(with: loaded)
    }

    /// Only for clusters whose inventory shows Flux, and roles that may use the Kubernetes API.
    /// The answer is shared (FluxStore) with the Flux screens and the Flux tile's sheet.
    func loadFlux(with client: TalosClient, id: String, inventory apps: ClusterInventory) async {
        guard model.allows(.workloads), fluxHinted(apps) else {
            flux = nil
            return
        }
        if flux == nil { flux = .loading }
        let key = model.fluxKey
        let loaded: LoadState<FluxStatus> = await .from { try await FluxStore.shared.load(with: client, key: key) }
        guard id == loadID else { return }
        flux = (flux ?? .loading).refreshed(with: loaded)
    }

    /// Only for clusters whose inventory shows Longhorn, Garage, CloudNativePG or another data service
    /// (a Data services plugin's app among them), and roles that may use the Kubernetes API: others make
    /// no Kubernetes call. A failed refresh keeps what was shown (with its error noted).
    func loadDataServices(with client: TalosClient, id: String, inventory apps: ClusterInventory) async {
        var catalog = PluginCatalog()
        if model.allows(.workloads) { catalog = await PluginCatalogCache.load(with: client) }
        guard id == loadID else { return }
        pluginCatalog = catalog
        let hints = dataServiceHints(apps, plugins: catalog)
        guard model.allows(.workloads), !hints.isEmpty else {
            dataServices = nil
            dataPlugins = []
            toolPlugins = []
            return
        }
        dataHints = hints
        if dataServices == nil { dataServices = .loading }
        async let detected = LoadState<PluginDetect>.from { try await client.pluginDetect(hints: hints) }
        let loaded: LoadState<DataServices> = await .from { try await client.dataServices(hints: hints) }
        let plugins = await detected
        guard id == loadID else { return }
        dataServices = (dataServices ?? .loading).refreshed(with: loaded)
        // A failed ask keeps the lines shown.
        if case .loaded(let detect, _, _) = plugins {
            (dataPlugins, toolPlugins) = (detect.dataServicePlugins, detect.homeTiles)
        }
    }

    /// Only for roles that may use the Kubernetes API, and clusters with an Alertmanager (searched
    /// once per cluster, see AlertsStore). A failure shows on the card only once one was found.
    func loadAlerts(with client: TalosClient, id: String) async {
        guard model.allows(.workloads) else {
            alerts = nil
            return
        }
        let key = model.alertsKey
        let fingerprint = model.activeSummary?.fingerprint ?? ""
        let loaded: LoadState<AMAlerts>?
        do {
            let read = try await AlertsStore.shared.load(with: client, key: key, fingerprint: fingerprint)
            loaded = read.map { LoadState<AMAlerts>.loaded($0, at: Date()) }
        } catch {
            loaded = alerts == nil ? nil : .failed(error.localizedDescription)
        }
        guard id == loadID else { return }
        alerts = loaded.map { (alerts ?? .loading).refreshed(with: $0) }
    }
}
