import SwiftUI
import IchorCore

/// Tailscale (os:admin): the operator, its connectors (subnet routers, exit nodes, app
/// connectors), proxy groups, proxy classes and the tailnet nameserver, problems first in each
/// section. A tap opens the object's summary; nothing is changed here.
struct TailscaleView: View {
    @Environment(AppModel.self) private var model
    @State private var state: LoadState<Tailscale> = .loading

    var body: some View {
        LoadStateView(state: state, retry: load) { ts in
            list(ts)
        }
        .navigationTitle(Text("Tailscale"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .refreshable { await load() }
    }

    private func load() async {
        guard let client = model.client else { return }
        let loaded: LoadState<Tailscale> = await .from { try await client.tailscale() }
        state = state.refreshed(with: loaded)
    }

    @ViewBuilder
    private func list(_ ts: Tailscale) -> some View {
        List {
            Section { header(ts) }
            if !ts.installed || ts.isEmpty {
                Section {
                    Text("The Tailscale operator is installed but runs no connector or proxy yet.").foregroundStyle(
                        .secondary)
                }
            } else {
                if !ts.connectors.isEmpty {
                    Section("Connectors") {
                        ForEach(ts.connectors) { ConnectorRow(connector: $0, version: ts.apiVersion) }
                    }
                }
                if !ts.proxyGroups.isEmpty {
                    Section("Proxy groups") {
                        ForEach(ts.proxyGroups) { ProxyGroupRow(group: $0, version: ts.apiVersion) }
                    }
                }
                if !ts.proxyClasses.isEmpty {
                    Section("Proxy classes") {
                        ForEach(ts.proxyClasses) { ProxyClassRow(proxyClass: $0, version: ts.apiVersion) }
                    }
                }
                if let dns = ts.dnsConfig {
                    Section("DNS") { DNSConfigRow(dns: dns, version: ts.apiVersion) }
                }
            }
        }
        .themedBackground()
    }

    /// The operator (ready/desired, image), the served version and the counts.
    @ViewBuilder
    private func header(_ ts: Tailscale) -> some View {
        if !ts.error.isEmpty {
            Text(verbatim: String(localized: "Some sections could not be read: \(ts.error)")).font(.callout)
                .foregroundStyle(.secondary)
        }
        if let op = ts.operator {
            let deployment = "\(op.namespace)/\(op.name)"
            let ratio = "\(op.ready)/\(op.desired)"
            HStack(alignment: .top, spacing: 12) {
                HealthDot(health: op.health).padding(.top, 5)
                Text(
                    verbatim: [
                        String(localized: "Operator \(deployment)"),
                        String(localized: "\(ratio) ready"),
                        op.image,
                    ]
                    .filter { !$0.isEmpty }.joined(separator: " · ")
                )
                .font(.callout).foregroundStyle(.secondary).lineLimit(2)
            }
        } else if ts.installed {
            Text("No operator Deployment found: nothing here is reconciled.").font(.callout).foregroundStyle(.secondary)
        }
        if !ts.version.isEmpty {
            Text(verbatim: String(localized: "API \(ts.version)")).font(.caption).foregroundStyle(.secondary)
        }
        if ts.installed && !ts.isEmpty {
            HStack(spacing: 12) {
                Text(verbatim: String(localized: "\(ts.counts.critical) critical"))
                    .font(.callout.weight(.medium))
                    .foregroundStyle(ts.counts.critical > 0 ? Color.statusBad : .secondary)
                Text(verbatim: String(localized: "\(ts.counts.warning) warning"))
                    .font(.callout.weight(.medium))
                    .foregroundStyle(ts.counts.warning > 0 ? Color.statusWarn : .secondary)
                Text(verbatim: String(localized: "\(ts.counts.ok) ok"))
                    .font(.callout.weight(.medium)).foregroundStyle(.secondary)
                Spacer()
            }
        }
        if ts.truncated {
            Text("Long list: only the first 200 objects of each kind are listed, problems first.")
                .font(.caption).foregroundStyle(.secondary)
        }
    }
}

/// One row: health dot and name, the details that matter, then (for a problem) the operator's
/// reason and message in full; pushes the object summary.
private struct RowShell: View {
    let kind: TailscaleKind
    let version: String
    let name: String
    let state: TailscaleState
    let line: [String?]

    var body: some View {
        NavigationLink {
            KubeObjectView(
                resource: KubeAPIResource(
                    group: TailscaleKind.group, version: version, resource: kind.resource, kind: kind.kind,
                    namespaced: false),
                namespace: "",
                name: name,
            )
        } label: {
            HStack(alignment: .top, spacing: 12) {
                HealthDot(health: state.health).padding(.top, 5)
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: name).font(.subheadline.monospaced()).lineLimit(1)
                    let details = line.compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · ")
                    if !details.isEmpty {
                        Text(verbatim: details).font(.caption).foregroundStyle(.secondary).lineLimit(3)
                    }
                    let problem = [state.reason, state.message].filter { !$0.isEmpty }.joined(separator: ": ")
                    if state.health.needsAttention && !problem.isEmpty {
                        Text(verbatim: problem).font(.caption)
                            .foregroundStyle(state.health == .critical ? Color.statusBad : .secondary)
                    }
                }
            }
        }
    }
}

private func readiness(_ ready: Bool) -> String {
    ready ? String(localized: "Ready") : String(localized: "Not ready")
}

/// A subnet router, exit node or app connector: what it does, its tailnet IPs and devices.
private struct ConnectorRow: View {
    let connector: TailscaleConnector
    let version: String

    var body: some View {
        let c = connector
        RowShell(
            kind: .connector, version: version, name: c.name, state: c.state,
            line: [
                c.hostname == c.name ? nil : c.hostname,
                c.routes.isEmpty ? nil : String(localized: "subnet router \(c.routes.joined(separator: ", "))"),
                c.exitNode ? String(localized: "exit node") : nil,
                c.appConnector ? String(localized: "app connector") : nil,
                c.appConnector ? c.appConnectorRoutes.joined(separator: ", ") : nil,
                c.tailnetIPs.joined(separator: ", "),
                String(localized: "\(c.devices.count) devices"),
                c.proxyClass.isEmpty ? nil : String(localized: "proxy class \(c.proxyClass)"),
            ])
    }
}

/// An egress, ingress or kube-apiserver proxy pool: its type, size, devices and URL.
private struct ProxyGroupRow: View {
    let group: TailscaleProxyGroup
    let version: String

    var body: some View {
        let g = group
        RowShell(
            kind: .proxyGroup, version: version, name: g.name, state: g.state,
            line: [
                g.type,
                String(localized: "\(g.replicas) replicas"),
                String(localized: "\(g.devices.count) devices"),
                g.url,
                g.proxyClass.isEmpty ? nil : String(localized: "proxy class \(g.proxyClass)"),
                readiness(g.state.ready),
                !g.state.ready && g.available ? String(localized: "some proxies running") : nil,
            ])
    }
}

/// A ProxyClass: whether it validates, and how many connectors and proxy groups name it.
private struct ProxyClassRow: View {
    let proxyClass: TailscaleProxyClass
    let version: String

    var body: some View {
        RowShell(
            kind: .proxyClass, version: version, name: proxyClass.name, state: proxyClass.state,
            line: [
                readiness(proxyClass.state.ready),
                String(localized: "used by \(proxyClass.usedBy)"),
            ])
    }
}

/// The DNSConfig: the nameserver that resolves tailnet names in the cluster.
private struct DNSConfigRow: View {
    let dns: TailscaleDNSConfig
    let version: String

    var body: some View {
        RowShell(
            kind: .dnsConfig, version: version, name: dns.name, state: dns.state,
            line: [
                dns.nameserverIP.isEmpty ? nil : String(localized: "nameserver \(dns.nameserverIP)"),
                readiness(dns.state.ready),
            ])
    }
}
