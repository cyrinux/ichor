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
    @State private var selected: DataServiceKind?
    /// How far the first read got, while there is nothing else to show.
    @State private var progress: ReadProgress?

    /// [selected]: the system to open on; nil (or not found) for the first.
    init(hints: String, downNodes: Set<String>, selected: DataServiceKind? = nil) {
        self.hints = hints
        self.downNodes = downNodes
        _selected = State(initialValue: selected)
    }

    var body: some View {
        LoadStateView(state: state, retry: load, progress: progress, stepLabel: Self.stepLabel) { services in
            let kinds = services.detected
            if kinds.isEmpty {
                // None of the systems the app reads runs here: which ones it reads, and how to ask for another.
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
            } else {
                let tab = selected.flatMap { kinds.contains($0) ? $0 : nil } ?? kinds[0]
                Group {
                    switch tab {
                    case .longhorn: LonghornList(status: services.longhorn!, refresh: load)
                    case .garage: GarageList(status: services.garage!, refresh: load)
                    case .cnpg: CnpgList(status: services.cnpg!, refresh: load)
                    case .dragonfly: DragonflyList(status: services.dragonfly!, refresh: load)
                    case .mariadb: MariaDbList(status: services.mariadb!, refresh: load)
                    case .percona: PerconaList(status: services.percona!, refresh: load)
                    case .certManager: CertificatesList(status: services.certManager!, refresh: load)
                    case .velero: VeleroList(status: services.velero!, refresh: load)
                    case .externalDns: ExternalDnsList(status: services.externalDns!, refresh: load)
                    case .sealedSecrets: SealedSecretsList(status: services.sealedSecrets!, refresh: load)
                    case .ceph: CephList(status: services.ceph!, refresh: load)
                    case .snapshots: SnapshotsList(status: services.snapshots!)
                    case .backupJobs: BackupJobsList(status: services.backupJobs!)
                    case .castai: CastAIList(status: services.castai!, refresh: load)
                    case .kubeGreen: KubeGreenList(status: services.kubeGreen!, refresh: load)
                    }
                }
                .safeAreaInset(edge: .top) {
                    VStack(alignment: .leading, spacing: 8) {
                        LikelyCauseBanner(causes: services.likelyCauses(downNodes: downNodes))
                        if kinds.count > 1 {
                            SystemPicker(kinds: kinds, selection: Binding(get: { tab }, set: { selected = $0 }))
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
        .navigationTitle(Text("Data services"))
        .navigationBarTitleDisplayMode(.inline)
    }

    private func load() async {
        guard let client = model.client else { return }
        let waiting: Bool = { if case .loading = state { return true } else { return false } }()
        let loaded: LoadState<DataServices> = await client.withReadProgress(
            TalosClient.dataServicesProgressOp, watching: waiting, update: { progress = $0 }
        ) {
            await .from { try await client.dataServices(hints: hints) }
        }
        progress = nil
        // A failed refresh keeps what was shown, with the error in the footer.
        state = state.refreshed(with: loaded)
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
        case "dragonfly": .dragonfly
        case "mariadb": .mariadb
        case "percona": .percona
        case "certmanager": .certManager
        case "sealedsecrets": .sealedSecrets
        case "velero": .velero
        case "externaldns": .externalDns
        case "kubegreen": .kubeGreen
        case "ceph": .ceph
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

/// One segment per system; past a few, segments get too narrow to read, so a menu instead.
private struct SystemPicker: View {
    let kinds: [DataServiceKind]
    @Binding var selection: DataServiceKind

    var body: some View {
        let picker = Picker(selection: $selection) {
            ForEach(kinds) { Text(verbatim: $0.tabTitle).tag($0) }
        } label: {
            EmptyView()
        }
        if kinds.count <= 3 {
            picker.pickerStyle(.segmented)
        } else {
            picker.pickerStyle(.menu).frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}
