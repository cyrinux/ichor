import SwiftUI
import IchorCore

/// The Flux tile's summary in its app sheet (os:admin): how many Kustomizations and
/// HelmReleases, the version, the state bar and counts, the failing ones, and the Flux screen.
/// Uses what the Overview or the Flux screen already loaded, else loads once when the sheet opens.
struct AppFluxSection: View {
    @Environment(AppModel.self) private var model
    @State private var state: LoadState<FluxStatus> = .loading

    private var store: FluxStore { .shared }

    var body: some View {
        content
            .task(id: model.fluxKey) {
                if let known = store.status(for: model.fluxKey) {
                    state = .loaded(known, at: Date())
                } else {
                    state = .loading
                    await load()
                }
            }
    }

    @ViewBuilder private var content: some View {
        switch state {
        case .loading:
            Section { Text("Reading Flux…").note() } header: { Text(verbatim: "GitOps") }
        case .failed(let error):
            Section { Text("Could not read: \(error)").note() } header: { Text(verbatim: "GitOps") }
        case .loaded(let status, _, _):
            if status.installed { summary(status) }
        }
    }

    private func summary(_ status: FluxStatus) -> some View {
        Section {
            VStack(alignment: .leading, spacing: 8) {
                HStack {
                    Text("\(status.apps.count) apps").font(.subheadline.weight(.semibold)).monospacedDigit()
                    if !status.version.isEmpty { InfoChip(text: status.version, monospaced: true) }
                    Spacer()
                    if status.worst.needsAttention { StatusPill(label: status.worst.label, color: status.worst.color) }
                }
                FluxStateBar(counts: status.stateCounts)
                Text(verbatim: (status.stateCounts.map { "\($0.count) \($0.state.label.lowercased())" } +
                                [String(localized: "\(status.sources.count) sources")]).joined(separator: " · "))
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .monospacedDigit()
            }
            ForEach(status.failing.prefix(3)) { app in
                VStack(alignment: .leading, spacing: 1) {
                    Label { Text(verbatim: [app.name, app.reason].filter { !$0.isEmpty }.joined(separator: " · ")).font(.callout) } icon: {
                        Image(systemName: FluxState.failing.symbol).foregroundStyle(.red)
                    }
                    if !app.message.isEmpty {
                        Text(verbatim: app.message).font(.caption).foregroundStyle(.secondary).lineLimit(2)
                    }
                }
            }
            NavigationLink {
                FluxView(downNodes: [])
            } label: {
                Label("Open Flux", systemImage: "arrow.triangle.branch")
            }
        } header: {
            Text(verbatim: "GitOps")
        }
    }

    private func load() async {
        guard let client = model.client else { return }
        let key = model.fluxKey
        let loaded: LoadState<FluxStatus> = await .from { try await store.load(with: client, key: key) }
        guard key == model.fluxKey else { return }
        state = state.refreshed(with: loaded)
    }
}
