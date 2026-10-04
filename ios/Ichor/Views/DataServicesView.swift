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

    var body: some View {
        LoadStateView(state: state, retry: load) { services in
            let kinds = services.detected
            if kinds.isEmpty {
                ContentUnavailableView("No data services found.", systemImage: "externaldrive.badge.questionmark")
                    .themedBackground()
            } else {
                let tab = selected.flatMap { kinds.contains($0) ? $0 : nil } ?? kinds[0]
                Group {
                    switch tab {
                    case .longhorn: LonghornList(status: services.longhorn!, refresh: load)
                    case .garage: GarageList(status: services.garage!, refresh: load)
                    case .cnpg: CnpgList(status: services.cnpg!, refresh: load)
                    case .dragonfly: DragonflyList(status: services.dragonfly!, refresh: load)
                    }
                }
                .safeAreaInset(edge: .top) {
                    VStack(alignment: .leading, spacing: 8) {
                        LikelyCauseBanner(causes: services.likelyCauses(downNodes: downNodes))
                        if kinds.count > 1 {
                            Picker(selection: Binding(get: { tab }, set: { selected = $0 })) {
                                ForEach(kinds) { Text(verbatim: $0.tabTitle).tag($0) }
                            } label: {
                                EmptyView()
                            }
                            .pickerStyle(.segmented)
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
        let loaded: LoadState<DataServices> = await .from { try await client.dataServices(hints: hints) }
        // A failed refresh keeps what was shown, with the error in the footer.
        state = state.refreshed(with: loaded)
    }
}

/// Relative time of a unix-ms instant ("3 days ago"), or never.
func relativeTime(_ millis: Int64) -> String {
    guard millis > 0 else { return String(localized: "never") }
    return Date(epochMillis: millis).formatted(.relative(presentation: .named))
}

/// A system that was found but could not be read: shown inside its own list only.
struct ErrorLine: View {
    let error: String

    var body: some View {
        if !error.isEmpty {
            Label { Text("Could not read: \(error)") } icon: { Image(systemName: "exclamationmark.triangle") }
                .font(.callout)
                .foregroundStyle(.red)
        }
    }
}
