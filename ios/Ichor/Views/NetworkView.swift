import SwiftUI
import IchorCore

/// A node's links, addresses, routes, DNS and time servers (os:reader). CNI/pod plumbing
/// is hidden unless asked for; a section that failed shows its error inline.
struct NetworkView: View {
    let node: String
    let hostname: String

    // Explicit: the private @State properties make the memberwise init private.
    init(node: String, hostname: String) {
        self.node = node
        self.hostname = hostname
    }

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<NodeNetwork> = .loading
    @State private var time: LoadState<NodeTimeInfo> = .loading
    @State private var showVirtual = false

    var body: some View {
        LoadStateView(state: state, retry: load) { network in
            List {
                Section {
                    NavigationLink {
                        ConnectionsView(node: node, hostname: hostname)
                    } label: {
                        Label("Connections", systemImage: "point.3.filled.connected.trianglepath.dotted")
                    }
                    Toggle("Show virtual interfaces", isOn: $showVirtual)
                } footer: {
                    Text("Virtual interfaces are the pod and CNI plumbing (veth, lxc, cilium, flannel…).")
                }
                linksSection(network)
                addressesSection(network)
                routesSection(network)
                dnsSection(network)
                timeSection(network)
            }
            .refreshable {
                await load()
                await loadTime()
            }
            .themedBackground()
        }
        .navigationTitle(String(localized: "Network · \(hostname)"))
        .navigationBarTitleDisplayMode(.inline)
        // Two tasks, so the clock check runs alongside the network listing.
        .task { await load() }
        .task { await loadTime() }
    }

    private func load() async {
        guard let client = model.client else { return }
        state = await .from { try await client.network(node: node) }
    }

    private func loadTime() async {
        guard let client = model.client else { return }
        time = await .from { try await client.nodeTime(node: node) }
    }

    private func linksSection(_ network: NodeNetwork) -> some View {
        let links = visibleLinks(network.links, showVirtual: showVirtual)
        let hidden = network.links.count - links.count
        return Section {
            SectionError(message: network.errors["links"])
            ForEach(links) { LinkRow(link: $0) }
        } header: {
            Text("Interfaces")
        } footer: {
            if hidden > 0 { Text("\(hidden) virtual interfaces hidden") }
        }
    }

    private func addressesSection(_ network: NodeNetwork) -> some View {
        Section("Addresses") {
            SectionError(message: network.errors["addresses"])
            ForEach(visibleAddresses(network.addresses, showVirtual: showVirtual)) { address in
                HStack(alignment: .firstTextBaseline) {
                    Text(verbatim: address.address).font(.body.monospaced()).textSelection(.enabled)
                    Spacer()
                    Text(verbatim: "\(address.link) · \(address.scope)").font(.caption).foregroundStyle(.secondary)
                }
            }
        }
    }

    private func routesSection(_ network: NodeNetwork) -> some View {
        let routes = visibleRoutes(network.routes, showVirtual: showVirtual)
        return Section("Routes") {
            SectionError(message: network.errors["routes"])
            ForEach(Array(routes.enumerated()), id: \.offset) { _, route in RouteRow(route: route) }
        }
    }

    private func dnsSection(_ network: NodeNetwork) -> some View {
        Section("DNS servers") {
            SectionError(message: network.errors["resolvers"])
            if network.resolvers.isEmpty && network.errors["resolvers"] == nil {
                Text("None").foregroundStyle(.secondary)
            }
            ForEach(network.resolvers, id: \.self) { Text(verbatim: $0).font(.body.monospaced()) }
        }
    }

    private func timeSection(_ network: NodeNetwork) -> some View {
        Section("Time servers") {
            SectionError(message: network.errors["timeServers"])
            if network.timeServers.isEmpty && network.errors["timeServers"] == nil {
                Text("None").foregroundStyle(.secondary)
            }
            ForEach(network.timeServers, id: \.self) { Text(verbatim: $0).font(.body.monospaced()) }
            switch time {
            case .loading:
                EmptyView()
            case .failed(let message):
                SectionError(message: message)
            case .loaded(let info, _):
                TimeOffsetRow(info: info, title: info.server.isEmpty ? String(localized: "Clock offset") : info.server)
            }
        }
    }
}

/// A failed section's error, in red; nothing when there is none.
struct SectionError: View {
    let message: String?

    var body: some View {
        if let message, !message.isEmpty {
            Label(message, systemImage: "exclamationmark.triangle")
                .font(.footnote)
                .foregroundStyle(.red)
        }
    }
}

private struct LinkRow: View {
    let link: NetLink

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack {
                Text(verbatim: link.name).font(.headline)
                if !link.kind.isEmpty {
                    Text(verbatim: link.kind).font(.caption).foregroundStyle(.secondary)
                }
                Spacer()
                StatusPill(label: link.state, color: link.isUp ? .green : link.state.lowercased() == "down" ? .red : .gray)
            }
            Text(verbatim: details).font(.caption.monospaced()).foregroundStyle(.secondary)
        }
    }

    private var details: String {
        var parts: [String] = []
        if !link.hardwareAddr.isEmpty { parts.append(link.hardwareAddr) }
        parts.append("MTU \(link.mtu)")
        if let speed = formatLinkSpeed(link.speedMbit) { parts.append(speed) }
        if link.type != "ether" && !link.type.isEmpty { parts.append(link.type) }
        return parts.joined(separator: "  ·  ")
    }
}

private struct RouteRow: View {
    let route: NetRoute

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack {
                if route.isDefault {
                    Label("Default route", systemImage: "arrow.triangle.turn.up.right.diamond.fill")
                        .font(.body.weight(.semibold))
                        .foregroundStyle(.blue)
                } else {
                    Text(verbatim: route.destination).font(.body.monospaced())
                }
                Spacer()
                Text(verbatim: route.family).font(.caption).foregroundStyle(.secondary)
            }
            Text(verbatim: details).font(.caption.monospaced()).foregroundStyle(.secondary)
        }
        .listRowBackground(route.isDefault ? Color.blue.opacity(0.08) : nil)
    }

    private var details: String {
        var parts: [String] = []
        if !route.gateway.isEmpty { parts.append("via \(route.gateway)") }
        if !route.link.isEmpty { parts.append("dev \(route.link)") }
        parts.append("metric \(route.metric)")
        return parts.joined(separator: " ")
    }
}

extension TimeDrift {
    var color: Color {
        switch self {
        case .ok: .green
        case .warning: .orange
        case .bad: .red
        }
    }
}

/// "time.cloudflare.com   +12 ms", coloured by drift, or the node's error.
struct TimeOffsetRow: View {
    let info: NodeTimeInfo
    let title: String

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack {
                Text(verbatim: title)
                Spacer()
                if info.error == nil {
                    Text(verbatim: formatOffset(info.offsetMs))
                        .font(.body.monospacedDigit().weight(.semibold))
                        .foregroundStyle(info.drift.color)
                } else {
                    StatusPill(label: String(localized: "Error"), color: .red)
                }
            }
            if let error = info.error {
                Text(error).font(.caption).foregroundStyle(.red)
            } else if !info.server.isEmpty && info.server != title {
                Text(verbatim: info.server).font(.caption).foregroundStyle(.secondary)
            }
        }
    }
}
