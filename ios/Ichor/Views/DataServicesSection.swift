import SwiftUI
import IchorCore

extension ServiceHealth {
    var color: Color {
        switch self {
        case .critical: .red
        case .warning: attentionColor
        case .ok: .green
        case .idle, .unknown: .secondary
        }
    }

    var label: String {
        switch self {
        case .critical: String(localized: "critical")
        case .warning: String(localized: "needs attention")
        case .ok: String(localized: "healthy")
        case .idle: String(localized: "idle")
        case .unknown: String(localized: "unknown")
        }
    }
}

extension GarageState {
    var label: String {
        switch self {
        case .healthy: String(localized: "Healthy")
        case .degraded: String(localized: "Degraded")
        case .unavailable: String(localized: "Unavailable")
        case .unknown: String(localized: "Unknown")
        }
    }
}

extension ClusterOverview {
    /// Hostnames of the nodes Talos reports not ready or unreachable: candidates for a likely cause.
    var downHostnames: Set<String> {
        Set(nodes.filter { $0.health != .ready }.map(\.hostname).filter { !$0.isEmpty })
    }
}

struct HealthDot: View {
    let health: ServiceHealth
    var size: CGFloat = 10

    var body: some View {
        Circle().fill(health.color).frame(width: size, height: size)
            .accessibilityLabel(Text(health.label))
    }
}

/// One line about a system: "12 volumes · 1 needs a look", "Degraded · 6/7 nodes up"...
func dataServiceSummary(_ kind: DataServiceKind, _ services: DataServices) -> String {
    guard let summary = services.summary(kind) else { return "" }
    if !summary.error.isEmpty && summary.total == 0 { return String(localized: "Could not read: \(summary.error)") }
    let attention = summary.attention > 0 ? String(localized: "\(summary.attention) need a look") : nil
    let head: String
    switch kind {
    case .longhorn: head = String(localized: "\(summary.total) volumes")
    case .cnpg: head = String(localized: "\(summary.total) Postgres clusters")
    case .dragonfly: head = String(localized: "\(summary.total) Dragonfly instances")
    case .mariadb: head = String(localized: "\(summary.total) MariaDB clusters")
    case .percona: head = String(localized: "\(summary.total) Percona clusters")
    case .certManager: head = String(localized: "\(summary.total) certificates")
    case .velero: head = String(localized: "\(summary.total) Velero schedules")
    case .ceph: head = String(localized: "\(summary.total) Ceph clusters")
    case .garage:
        // One Garage cluster: its own state says more than "1 cluster".
        if let single = services.garage?.instances.first, services.garage?.instances.count == 1 {
            let nodes = single.storageNodes > 0 ? String(localized: "\(single.storageNodesUp)/\(single.storageNodes) nodes up") : nil
            return [single.state.label, nodes].compactMap { $0 }.joined(separator: " · ")
        }
        head = String(localized: "\(summary.total) clusters")
    }
    return [head, attention].compactMap { $0 }.joined(separator: " · ")
}

/// "worker-3 is not ready: the likely cause of 3 problems", one line per node.
struct LikelyCauseBanner: View {
    let causes: [LikelyCause]

    var body: some View {
        if !causes.isEmpty {
            Label {
                Text(verbatim: causes.map {
                    String(localized: "\($0.node) is not ready") + ": " + String(localized: "the likely cause of \($0.problems) problems")
                }.joined(separator: "\n"))
            } icon: {
                Image(systemName: "exclamationmark.triangle.fill")
            }
            .font(.callout)
            .foregroundStyle(.statusBad)
        }
    }
}

/// The overview's Data services row: Longhorn, Garage and CloudNativePG at a glance, opening the
/// Data services screen (a system's row opens on its tab). Only shown when the inventory has one of
/// them; a skeleton while loading.
struct DataServicesSection: View {
    let state: LoadState<DataServices>
    /// Catalog ids from the inventory, passed on to the screen.
    let hints: String
    let apps: [String: InventoryApp]
    let downNodes: Set<String>

    private var hinted: [DataServiceKind] { DataServiceKind.allCases.filter { hints.split(separator: ",").map(String.init).contains($0.catalogID) } }

    var body: some View {
        switch state {
        case .loading:
            Section {
                VStack(alignment: .leading, spacing: 10) {
                    Text("Data services").font(.headline)
                    ForEach(hinted) { line($0, text: String(localized: "Data services"), health: nil) }
                }
                .redacted(reason: .placeholder)
            }
        case .failed(let message):
            Section {
                NavigationLink(value: Route.dataServices(hints: hints, downNodes: downNodes)) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("Data services").font(.headline)
                        Text("Could not read: \(message)").font(.caption).foregroundStyle(.secondary).lineLimit(2)
                    }
                }
            }
        case .loaded(let services, _, _):
            if !services.detected.isEmpty {
                Section {
                    NavigationLink(value: Route.dataServices(hints: hints, downNodes: downNodes)) { header(services) }
                    // One row per system, opening the screen on its tab.
                    ForEach(services.detected) { kind in
                        NavigationLink(value: Route.dataServices(hints: hints, downNodes: downNodes, kind: kind)) {
                            line(kind, text: dataServiceSummary(kind, services), health: services.summary(kind)?.health)
                        }
                    }
                }
            }
        }
    }

    private func header(_ services: DataServices) -> some View {
        let worst = services.worst
        return VStack(alignment: .leading, spacing: 10) {
            HStack {
                Text("Data services").font(.headline)
                Spacer()
                if worst.needsAttention { StatusPill(label: worst.label, color: worst.color) }
            }
            LikelyCauseBanner(causes: services.likelyCauses(downNodes: downNodes))
        }
        .padding(.vertical, 4)
    }

    private func line(_ kind: DataServiceKind, text: String, health: ServiceHealth?) -> some View {
        HStack(spacing: 12) {
            KindIcon(kind: kind, app: apps[kind.catalogID], size: 28)
            VStack(alignment: .leading, spacing: 1) {
                Text(verbatim: kind.title).font(.subheadline)
                Text(verbatim: text).font(.caption).foregroundStyle(.secondary).lineLimit(2).monospacedDigit()
            }
            Spacer()
            if let health { HealthDot(health: health) }
        }
    }
}

/// The app's own icon when the inventory has it, else a generic symbol.
struct KindIcon: View {
    let kind: DataServiceKind
    let app: InventoryApp?
    let size: CGFloat

    var body: some View {
        if let app {
            AppIconView(app: app, size: size)
        } else {
            Image(systemName: symbol)
                .font(.system(size: size * 0.5))
                .foregroundStyle(.secondary)
                .frame(width: size, height: size)
                .background(.quaternary, in: RoundedRectangle(cornerRadius: size * 0.3))
        }
    }

    private var symbol: String {
        switch kind {
        case .longhorn: "externaldrive"
        case .garage: "cloud"
        case .cnpg: "cylinder.split.1x2"
        case .dragonfly: "memorychip"
        case .mariadb: "tablecells"
        case .percona: "point.3.connected.trianglepath.dotted"
        case .certManager: "checkmark.seal"
        case .velero: "clock.arrow.circlepath"
        case .ceph: "internaldrive"
        }
    }
}
