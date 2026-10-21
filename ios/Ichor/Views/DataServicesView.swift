import SwiftUI
import IchorCore

/// Longhorn volumes, Garage clusters and CloudNativePG clusters, one segment per system the cluster
/// runs, through the Kubernetes API with the admin kubeconfig Talos issues (os:admin). A node that is
/// not ready and explains the problems is named above the segments.
struct DataServicesView: View {
    /// Catalog ids from the inventory ("" checks everything) and the hostnames Talos reports down.
    let hints: String
    let downNodes: Set<String>

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<DataServices> = .loading
    /// The CRD plugins of the data-services category: one tab each, after the
    /// native systems.
    @State private var plugins: LoadState<PluginDetect> = .loading
    @State private var selected: DataServicesTab.ID?
    /// Refreshes asked here: a plugin tab reads again in place.
    @State private var refreshes = 0
    /// How far the first read got, while there is nothing else to show.
    @State private var progress: ReadProgress?
    /// The one system shown, without the picker: one the Kubernetes home files in another family.
    private let solo: DataServiceKind?

    /// [selected]: the system to open on; nil (or not found) for the first. [solo]: that system alone.
    init(hints: String, downNodes: Set<String>, selected: DataServiceKind? = nil, solo: Bool = false) {
        self.hints = hints
        self.downNodes = downNodes
        self.solo = solo ? selected : nil
        _selected = State(initialValue: selected.map { DataServicesTab.native($0).id })
    }

    /// [plugin]: the id of the plugin whose tab to open on.
    init(hints: String, downNodes: Set<String>, plugin: String) {
        self.hints = hints
        self.downNodes = downNodes
        solo = nil
        _selected = State(initialValue: DataServicesTab.plugin(id: plugin, name: "").id)
    }

    var body: some View {
        LoadStateView(state: state, retry: refresh, progress: progress, stepLabel: Self.stepLabel,
                      showsFooter: !pluginTabShown) { services in
            let tabs = hubTabs(services)
            if tabs.isEmpty {
                switch plugins {
                case .loading:
                    // The plugins may still name one.
                    SkeletonView()
                case .failed(let message):
                    // No native system, and the plugins could not be asked: say why, not "none".
                    ContentUnavailableView {
                        Label("Request failed", systemImage: "exclamationmark.triangle")
                    } description: {
                        Text(message)
                    } actions: {
                        Button("Retry") { Task { await refresh() } }
                    }
                case .loaded:
                    noDataServices
                }
            } else {
                let tab = Self.tab(in: tabs, selected: selected)
                Group {
                    switch tab {
                    case .native(let kind): nativeList(kind, services)
                    case .plugin(let id, _):
                        PluginList(pluginID: id, reload: pluginReload).id(id)
                    }
                }
                .safeAreaInset(edge: .top) {
                    VStack(alignment: .leading, spacing: 8) {
                        LikelyCauseBanner(causes: services.likelyCauses(downNodes: downNodes))
                        if tabs.count > 1 {
                            SystemPicker(tabs: tabs, selection: Binding<String?>(get: { tab.id }, set: { selected = $0 }))
                        }
                    }
                    .padding(.horizontal)
                    .padding(.bottom, 6)
                    .background(.bar)
                }
            }
        }
        .task { await load() }
        // A new API address (set on the Kubernetes screen): read again through it.
        .id(model.client?.kubeServer)
        .navigationTitle(solo.map { Text(verbatim: $0.title) } ?? Text("Data services"))
        .navigationBarTitleDisplayMode(.inline)
    }

    /// None of the systems the app reads runs here: which ones it reads, and how to ask for another.
    private var noDataServices: some View {
        ContentUnavailableView {
            Label("No data services found.", systemImage: "externaldrive.badge.questionmark")
        } description: {
            Text("None of the systems Ichor can read runs on this cluster. See the supported ones, or ask for the one you use.")
        } actions: {
            NavigationLink("Integrations", value: Route.supportedIntegrations)
            NavigationLink("Request an integration…", value: Route.integrations)
        }
        .buttonStyle(.bordered)
        .themedBackground()
    }

    @ViewBuilder
    private func nativeList(_ kind: DataServiceKind, _ services: DataServices) -> some View {
        switch kind {
        case .longhorn: LonghornList(status: services.longhorn!, refresh: refresh)
        case .garage: GarageList(status: services.garage!, refresh: refresh)
        case .cnpg: CnpgList(status: services.cnpg!, refresh: refresh)
        case .certManager: CertificatesList(status: services.certManager!, refresh: refresh)
        case .velero: VeleroList(status: services.velero!, refresh: refresh)
        case .snapshots: SnapshotsList(status: services.snapshots!)
        case .backupJobs: BackupJobsList(status: services.backupJobs!)
        case .castai: CastAIList(status: services.castai!, refresh: refresh)
        }
    }

    /// The tabs: storage, databases and backups on a Kubernetes cluster, whose home files the
    /// others in their families; every system on a Talos cluster; `solo` alone.
    private func hubTabs(_ services: DataServices) -> [DataServicesTab] {
        DataServicesTab.hubTabs(services, plugins: loadedPlugins, kube: model.activeIsKube, solo: solo)
    }

    /// The tab shown: the one picked, else the first.
    private static func tab(in tabs: [DataServicesTab], selected: DataServicesTab.ID?) -> DataServicesTab {
        tabs.first { $0.id == selected } ?? tabs[0]
    }

    /// A plugin tab is on screen: its own list shows the freshness footer, the screen's does not.
    private var pluginTabShown: Bool {
        guard case .loaded(let services, _, _) = state else { return false }
        let tabs = hubTabs(services)
        guard !tabs.isEmpty, case .plugin = Self.tab(in: tabs, selected: selected) else { return false }
        return true
    }

    /// Which plugins run, once asked (nil while asking, or when it failed).
    private var loadedPlugins: PluginDetect? {
        guard case .loaded(let detect, _, _) = plugins else { return nil }
        return detect
    }

    /// A plugin tab starts over on another cluster or screenshot mode switch, and reads in
    /// place after a refresh here.
    private var pluginReload: PluginReload {
        PluginReload(scope: "\(model.activeContext)#\(model.dataGeneration)", bump: refreshes)
    }

    /// Everything again: the systems, which plugins run, and the open plugin tab (in place).
    private func refresh() async {
        refreshes += 1
        await load()
    }

    private func load() async {
        guard let client = model.client else { return }
        let hints = hints
        async let detected = LoadState<PluginDetect>.from { try await client.pluginDetect(hints: hints) }
        let waiting: Bool = { if case .loading = state { return true } else { return false } }()
        let loaded: LoadState<DataServices> = await client.withReadProgress(
            TalosClient.dataServicesProgressOp, watching: waiting, update: { progress = $0 }
        ) {
            await .from { try await client.dataServices(hints: hints) }
        }
        progress = nil
        // A failed refresh keeps what was shown, with the error in the footer.
        state = state.refreshed(with: loaded)
        plugins = plugins.refreshed(with: await detected)
    }

    /// A read step by name: the shared reads, then each system by the name its segment shows;
    /// an unknown one by its id.
    private static func stepLabel(_ id: String) -> String {
        switch id {
        case "api": String(localized: "API groups")
        case "pods": String(localized: "Reading pods")
        default: dataServiceStep(id)?.title ?? id
        }
    }

    /// The system a Go step id reads (step ids are short words, not catalog ids).
    private static func dataServiceStep(_ id: String) -> DataServiceKind? {
        switch id {
        case "snapshots": .snapshots
        case "backups": .backupJobs
        case "longhorn": .longhorn
        case "cnpg": .cnpg
        case "certmanager": .certManager
        case "velero": .velero
        case "castai": .castai
        case "garage": .garage
        default: nil
        }
    }
}

/// Relative time of a unix-ms instant ("3 days ago"), or never.
/// A system that was found but could not be read: shown inside its own list only.
struct ErrorLine: View {
    let error: String

    var body: some View {
        if !error.isEmpty {
            Label { Text("Could not read: \(error)") } icon: { Image(systemName: "exclamationmark.triangle") }
                .font(.callout)
                .foregroundStyle(.statusBad)
        }
    }
}

/// One segment per system or plugin; past a few, segments get too narrow to read, so a menu instead.
private struct SystemPicker: View {
    let tabs: [DataServicesTab]
    @Binding var selection: DataServicesTab.ID?

    var body: some View {
        let picker = Picker(selection: $selection) {
            ForEach(tabs) { tab in Text(verbatim: tab.localizedTitle).tag(Optional(tab.id)) }
        } label: {
            EmptyView()
        }
        if tabs.count <= 3 {
            picker.pickerStyle(.segmented)
        } else {
            picker.pickerStyle(.menu).frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

private extension DataServicesTab {
    /// A system's short name, or the plugin's own (translated by Go).
    var localizedTitle: String {
        switch self {
        case .native(let kind): kind.localizedTabTitle
        case .plugin(_, let name): name
        }
    }
}

private extension DataServiceKind {
    /// The picker's name: the core's short product name, or a translated one where the full
    /// name ("CSI VolumeSnapshots", "Backup CronJobs") describes the list it heads.
    var localizedTabTitle: String {
        switch self {
        case .snapshots: String(localized: "Snapshots")
        case .backupJobs: String(localized: "Backup jobs")
        default: tabTitle
        }
    }
}
