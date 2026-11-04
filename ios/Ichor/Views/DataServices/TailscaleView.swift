import SwiftUI
import IchorCore

/// Tailscale (os:admin): the operator, what it exposes on the tailnet (and reaches on it), its
/// connectors (subnet routers, exit nodes, app connectors), proxy groups, proxy classes and the
/// tailnet nameserver, problems first in each section. A tap opens the object's summary;
/// nothing is changed here.
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
                if !ts.exposed.isEmpty {
                    Section("Exposed on the tailnet") {
                        ForEach(ts.exposed) { ExposedRow(exposed: $0) }
                    }
                }
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

/// How many devices a row lists before folding the rest into a count.
private let devicesShown = 3

/// The object summary's resource for a cluster-scoped tailscale.com kind.
private func tailscaleResource(_ kind: TailscaleKind, _ version: String) -> KubeAPIResource {
    KubeAPIResource(
        group: TailscaleKind.group, version: version, resource: kind.resource, kind: kind.kind, namespaced: false)
}

/// One row: health dot and title, the details that matter, the devices it runs, then (for a
/// problem) the operator's reason and message in full; pushes the object summary.
private struct RowShell: View {
    let resource: KubeAPIResource
    var namespace = ""
    let name: String
    var title: String?
    let state: TailscaleState
    let line: [String?]
    var devices: [TailnetDevice] = []
    /// The tap destination; the object summary unless a row points somewhere else (an exposed
    /// row opens its network map).
    var destination: AnyView? = nil

    var body: some View {
        NavigationLink {
            if let destination {
                destination
            } else {
                KubeObjectView(resource: resource, namespace: namespace, name: name)
            }
        } label: {
            HStack(alignment: .top, spacing: 12) {
                HealthDot(health: state.health).padding(.top, 5)
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: title ?? name).font(.subheadline.monospaced()).lineLimit(1)
                    let details = line.compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · ")
                    if !details.isEmpty {
                        Text(verbatim: details).font(.caption).foregroundStyle(.secondary).lineLimit(3)
                    }
                    ForEach(Array(devices.prefix(devicesShown).enumerated()), id: \.offset) { _, d in
                        let ips = d.tailnetIPs.joined(separator: ", ")
                        Text(verbatim: [d.hostname, ips].filter { !$0.isEmpty }.joined(separator: "  "))
                            .font(.caption.monospaced()).foregroundStyle(.secondary).lineLimit(1)
                    }
                    if devices.count > devicesShown {
                        let more = devices.count - devicesShown
                        Text(verbatim: String(localized: "\(more) more devices")).font(.caption)
                            .foregroundStyle(.secondary)
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

/// A Service or Ingress on the tailnet: its tailnet name as the title, then where it lives, its
/// ports, the ProxyGroup serving it and its tags. An egress row is titled by the Service and says
/// which tailnet target it reaches.
private struct ExposedRow: View {
    let exposed: TailscaleExposed

    var body: some View {
        let e = exposed
        let objectName = "\(e.namespace)/\(e.name)"
        let title = e.isEgress ? objectName : (e.tailnetNames.first ?? objectName)
        let target = e.tailnetNames.first ?? ""
        let group = e.proxyGroup
        RowShell(
            resource: e.resource, namespace: e.namespace, name: e.name, title: title, state: e.state,
            line: [
                e.isEgress && !target.isEmpty ? String(localized: "to \(target)") : nil,
                objectName == title ? nil : objectName,
                e.isIngress ? e.kind : nil,
                e.isEgress ? nil : e.tailnetNames.dropFirst().joined(separator: ", "),
                e.ports.joined(separator: ", "),
                group.isEmpty ? nil : String(localized: "via \(group)"),
                e.tags.joined(separator: ", "),
            ],
            destination: AnyView(TailscaleNetworkView(kind: e.kind, namespace: e.namespace, name: e.name, title: title)))
    }
}

/// A subnet router, exit node or app connector: what it does, its tailnet IPs and devices.
private struct ConnectorRow: View {
    let connector: TailscaleConnector
    let version: String

    var body: some View {
        let c = connector
        RowShell(
            resource: tailscaleResource(.connector, version), name: c.name, state: c.state,
            line: [
                c.hostname == c.name ? nil : c.hostname,
                c.routes.isEmpty ? nil : String(localized: "subnet router \(c.routes.joined(separator: ", "))"),
                c.exitNode ? String(localized: "exit node") : nil,
                c.appConnector ? String(localized: "app connector") : nil,
                c.appConnector ? c.appConnectorRoutes.joined(separator: ", ") : nil,
                c.tailnetIPs.joined(separator: ", "),
                String(localized: "\(c.devices.count) devices"),
                c.proxyClass.isEmpty ? nil : String(localized: "proxy class \(c.proxyClass)"),
            ],
            devices: c.devices)
    }
}

/// An egress, ingress or kube-apiserver proxy pool: its type, size, devices and URL.
private struct ProxyGroupRow: View {
    let group: TailscaleProxyGroup
    let version: String

    var body: some View {
        let g = group
        RowShell(
            resource: tailscaleResource(.proxyGroup, version), name: g.name, state: g.state,
            line: [
                g.type,
                String(localized: "\(g.replicas) replicas"),
                String(localized: "\(g.devices.count) devices"),
                g.serves > 0 ? String(localized: "serves \(g.serves)") : nil,
                g.url,
                g.proxyClass.isEmpty ? nil : String(localized: "proxy class \(g.proxyClass)"),
                readiness(g.state.ready),
                !g.state.ready && g.available ? String(localized: "some proxies running") : nil,
            ],
            devices: g.devices)
    }
}

/// A ProxyClass: whether it validates, and how many connectors and proxy groups name it.
private struct ProxyClassRow: View {
    let proxyClass: TailscaleProxyClass
    let version: String

    var body: some View {
        RowShell(
            resource: tailscaleResource(.proxyClass, version), name: proxyClass.name, state: proxyClass.state,
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
            resource: tailscaleResource(.dnsConfig, version), name: dns.name, state: dns.state,
            line: [
                dns.nameserverIP.isEmpty ? nil : String(localized: "nameserver \(dns.nameserverIP)"),
                readiness(dns.state.ready),
            ])
    }
}
