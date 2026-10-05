import SwiftUI
import IchorCore

/// The overview's Flux row: the Kustomizations' and HelmReleases' states as a bar, the counts
/// (ready, reconciling, failing, suspended), and at most two failing ones with their reason;
/// opens the Flux screen. Only shown when the inventory has Flux (and the role may use the
/// Kubernetes API); one calm line when everything is ready.
struct FluxSection: View {
    let state: LoadState<FluxStatus>
    /// Flux's own inventory app, for its icon.
    let app: InventoryApp?
    let downNodes: Set<String>

    var body: some View {
        switch state {
        case .loading:
            Section {
                VStack(alignment: .leading, spacing: 10) {
                    header(count: 0)
                    FluxStateBar(counts: [(state: .ready, count: 1)])
                    Text(verbatim: "Flux objects ready").font(.caption)
                }
                .redacted(reason: .placeholder)
            }
        case .failed(let message):
            Section {
                NavigationLink(value: Route.flux(downNodes: downNodes)) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(verbatim: "Flux").font(.headline)
                        Text("Could not read: \(message)").font(.caption).foregroundStyle(.secondary).lineLimit(2)
                    }
                }
            }
        case .loaded(let status, _, _):
            if status.installed && !status.apps.isEmpty {
                Section {
                    NavigationLink(value: Route.flux(downNodes: downNodes)) { content(status) }
                }
            }
        }
    }

    private func header(count: Int) -> some View {
        HStack(spacing: 10) {
            if let app {
                AppIconView(app: app, size: 28)
            } else {
                Image(systemName: "arrow.triangle.branch")
                    .frame(width: 28, height: 28)
                    .background(.quaternary, in: RoundedRectangle(cornerRadius: 8))
            }
            Text(verbatim: "Flux").font(.headline)
            Spacer()
            Text("\(count) apps").font(.subheadline).foregroundStyle(.secondary).monospacedDigit()
        }
    }

    private func content(_ status: FluxStatus) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            header(count: status.apps.count)
            FluxStateBar(counts: status.stateCounts)
            if status.allCalm && status.failingSources.isEmpty {
                Text("All Kustomizations and HelmReleases ready").font(.caption).foregroundStyle(.secondary)
            } else {
                counts(status)
                ForEach(status.failing.prefix(2)) { problem($0) }
                if status.failing.isEmpty, let source = status.failingSources.first { failingSource(source) }
            }
        }
        .padding(.vertical, 4)
    }

    private func counts(_ status: FluxStatus) -> some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 12) { countItems(status) }
            VStack(alignment: .leading, spacing: 2) { countItems(status) }
        }
        .font(.caption)
        .monospacedDigit()
    }

    @ViewBuilder private func countItems(_ status: FluxStatus) -> some View {
        ForEach(status.stateCounts.indices, id: \.self) { i in
            let part = status.stateCounts[i]
            HStack(spacing: 4) {
                Circle().fill(part.state.color).frame(width: 7, height: 7)
                    .accessibilityHidden(true)
                Text(verbatim: "\(part.count) \(part.state.label.lowercased())")
            }
        }
    }

    private func problem(_ app: FluxApp) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(app.level.color).font(.caption)
            VStack(alignment: .leading, spacing: 1) {
                Text(verbatim: [app.name, app.reason].filter { !$0.isEmpty }.joined(separator: " · "))
                    .font(.caption.weight(.semibold))
                    .lineLimit(1)
                if !app.message.isEmpty {
                    Text(verbatim: app.message)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                }
            }
        }
    }

    private func failingSource(_ source: FluxSource) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Image(systemName: fluxKindSymbol(source.kind)).foregroundStyle(source.level.color).font(.caption)
            VStack(alignment: .leading, spacing: 1) {
                Text(verbatim: [source.name, source.reason].filter { !$0.isEmpty }.joined(separator: " · "))
                    .font(.caption.weight(.semibold))
                    .lineLimit(1)
                if !source.message.isEmpty {
                    Text(verbatim: source.message).font(.caption).foregroundStyle(.secondary).lineLimit(2)
                }
            }
        }
    }
}
