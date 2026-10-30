import SwiftUI
import IchorCore

/// What gives LoadBalancer Services their address, problems first: each Service that waits,
/// with why; then the pools and what is left of them, who advertises them, MetalLB's BGP
/// peers, kube-vip's ranges and leases, and every LoadBalancer Service (it opens its summary).
struct KubeLoadBalancingView: View {
    @Environment(AppModel.self) private var model
    @State private var state: LoadState<KubeLoadBalancing> = .loading

    var body: some View {
        LoadStateView(state: state, retry: { await load() }) { lb in
            if lb.isEmpty {
                ContentUnavailableView {
                    Label(LoadBalancingText.title, systemImage: "point.3.connected.trianglepath.dotted")
                } description: {
                    Text(verbatim: LoadBalancingText.empty)
                }
            } else {
                list(lb)
            }
        }
        .navigationTitle(Text(verbatim: LoadBalancingText.title))
        .navigationBarTitleDisplayMode(.inline)
        .task(id: kubeNamespacesKey(model)) { await load() }
    }

    private func list(_ lb: KubeLoadBalancing) -> some View {
        List {
            if lb.partialAccess {
                Text(verbatim: LoadBalancingText.partial).font(.footnote).foregroundStyle(.secondary)
            }
            ForEach(Array(lb.configErrors.enumerated()), id: \.offset) { _, e in
                VStack(alignment: .leading, spacing: 4) {
                    Text(
                        verbatim: LoadBalancingText.configError(
                            e.node.isEmpty ? e.component : "\(e.component) · \(e.node)")
                    )
                    .font(.headline).foregroundStyle(Color.statusBad)
                    if !e.message.isEmpty { Text(verbatim: e.message).font(.caption).foregroundStyle(.secondary) }
                }
            }
            ForEach(lb.pending) { svc in
                serviceLink(svc) { WaitingRow(service: svc) }
            }
            if !lb.pools.isEmpty {
                Section(LoadBalancingText.pools) { ForEach(lb.pools) { PoolRow(pool: $0) } }
            }
            if !lb.advertisements.isEmpty {
                Section(LoadBalancingText.advertisements) { ForEach(lb.advertisements) { AdvertisementRow(ad: $0) } }
            }
            if !lb.bgpPeers.isEmpty {
                Section(LoadBalancingText.bgpPeers) { ForEach(lb.bgpPeers) { PeerRow(peer: $0) } }
            }
            if let vip = lb.kubeVip {
                Section(LoadBalancingText.kubeVip) { KubeVipRows(vip: vip) }
            }
            if !lb.services.isEmpty {
                Section(LoadBalancingText.services) {
                    ForEach(lb.services) { svc in serviceLink(svc) { ServiceLine(service: svc) } }
                }
            }
        }
        .refreshable { await load() }
        .themedBackground()
    }

    private func serviceLink<Label: View>(_ svc: LBService, @ViewBuilder label: () -> Label) -> some View {
        NavigationLink {
            KubeObjectView(
                resource: KubeAPIResource(resource: "services", kind: "Service"), namespace: svc.namespace,
                name: svc.name)
        } label: {
            label()
        }
    }

    private func load() async {
        guard let client = model.client else { return }
        let fetched: LoadState<KubeLoadBalancing> = await .from { try await client.loadBalancing() }
        guard !Task.isCancelled else { return }
        state = state.refreshed(with: fetched)
    }
}

/// One sentence on why a Service waits for an address.
func lbCauseText(_ cause: LBCause) -> String {
    switch cause {
    case .noController: LoadBalancingText.causeNoController
    case .configError: LoadBalancingText.causeConfigError
    case .controllerDown: LoadBalancingText.causeControllerDown
    case .noPool: LoadBalancingText.causeNoPool
    case .poolNotMatched: LoadBalancingText.causePoolNotMatched
    case .poolExhausted: LoadBalancingText.causePoolExhausted
    case .poolNotAdvertised: LoadBalancingText.causePoolNotAdvertised
    case .unknown: LoadBalancingText.causeUnknown
    }
}

private func lbLevelColor(_ level: StorageLevel) -> Color {
    switch level {
    case .critical: .statusBad
    case .warning: .statusWarn
    case .ok: .secondary
    }
}

private struct WaitingRow: View {
    let service: LBService

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(verbatim: LoadBalancingText.waiting(service.id)).font(.headline).foregroundStyle(
                lbLevelColor(service.level))
            Text(verbatim: lbCauseText(service.cause)).font(.callout)
            let age = checkupAge(service.since, now: Int64(Date().timeIntervalSince1970 * 1000))
            if !age.isEmpty {
                Text(verbatim: LoadBalancingText.waitingSince(age)).font(.caption).foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

private struct PoolRow: View {
    let pool: LBPool

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline) {
                Text(verbatim: pool.name).font(.callout.monospaced()).lineLimit(1)
                Spacer()
                Text(verbatim: pool.controller).font(.caption).foregroundStyle(.secondary)
                Text(verbatim: LoadBalancingText.poolFree("\(pool.available)", "\(pool.total)"))
                    .font(.caption).foregroundStyle(lbLevelColor(pool.level))
            }
            ProgressView(value: pool.used)
            Text(verbatim: pool.addresses.joined(separator: ", ")).font(.caption.monospaced()).foregroundStyle(
                .secondary
            ).lineLimit(2)
            let notes = poolNotes
            if !notes.isEmpty {
                Text(verbatim: notes.joined(separator: " · ")).font(.caption).foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .combine)
    }

    private var poolNotes: [String] {
        var notes: [String] = []
        if pool.controller == LBController.metallb.rawValue {
            notes.append(
                pool.advertisedBy.isEmpty
                    ? LoadBalancingText.poolNotAdvertised
                    : LoadBalancingText.poolAdvertisedBy(pool.advertisedBy.joined(separator: ", ")))
        }
        if !pool.autoAssign { notes.append(LoadBalancingText.poolManual) }
        if !pool.namespaces.isEmpty {
            notes.append(LoadBalancingText.poolNamespaces(pool.namespaces.joined(separator: ", ")))
        }
        return notes
    }
}

private struct AdvertisementRow: View {
    let ad: LBAdvertisement

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack {
                Text(verbatim: ad.name).font(.callout.monospaced())
                Spacer()
                let kind = ad.kind == "bgp" ? "BGP" : "L2"
                Text(verbatim: ad.controller == "cilium" ? "Cilium \(kind)" : kind).font(.caption)
            }
            if ad.allPools || !ad.pools.isEmpty {
                Text(
                    verbatim: ad.allPools
                        ? LoadBalancingText.advAllPools : LoadBalancingText.advPools(ad.pools.joined(separator: ", "))
                )
                .font(.caption).foregroundStyle(.secondary)
            }
            let via = ad.interfaces.isEmpty ? ad.peers : ad.interfaces
            if !via.isEmpty {
                Text(verbatim: LoadBalancingText.advVia(via.joined(separator: ", "))).font(.caption).foregroundStyle(
                    .secondary)
            }
            if let reason = inactiveText {
                Text(verbatim: reason).font(.caption).foregroundStyle(.red)
            }
        }
        .accessibilityElement(children: .combine)
    }

    /// Why a Cilium L2 policy announces nothing, nil when it works.
    private var inactiveText: String? {
        switch ad.inactive {
        case "l2Disabled": LoadBalancingText.advL2Disabled
        case "noLoadBalancerIPs": LoadBalancingText.advNoLBIPs
        case "noService": LoadBalancingText.advNoService
        default: nil
        }
    }
}

private struct PeerRow: View {
    let peer: LBBGPPeer

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack {
                Text(verbatim: peer.name).font(.callout.monospaced())
                Spacer()
                Text(verbatim: peer.peerAddress.isEmpty ? peer.interface : peer.peerAddress).font(.caption.monospaced())
            }
            Text(
                verbatim: LoadBalancingText.peerASN(
                    peer.peerASN > 0 ? "\(peer.peerASN)" : peer.dynamicASN, "\(peer.myASN)")
            )
            .font(.caption).foregroundStyle(.secondary)
            if !peer.sessionsKnown {
                Text(verbatim: LoadBalancingText.peerSessionsUnknown).font(.caption).foregroundStyle(.secondary)
            } else if peer.sessions.isEmpty {
                Text(verbatim: LoadBalancingText.peerNoSession).font(.caption).foregroundStyle(.secondary)
            } else {
                ForEach(peer.sessions, id: \.node) { s in
                    Text(
                        verbatim: s.up
                            ? LoadBalancingText.peerSessionUp(s.node) : LoadBalancingText.peerSessionDown(s.node)
                    )
                    .font(.caption).foregroundStyle(s.up ? Color.primary : Color.statusBad)
                }
            }
        }
        .accessibilityElement(children: .combine)
    }
}

private struct KubeVipRows: View {
    let vip: LBKubeVip

    var body: some View {
        if vip.ranges.isEmpty {
            Text(verbatim: LoadBalancingText.kubeVipNoRange).font(.caption).foregroundStyle(.secondary)
        }
        ForEach(vip.ranges, id: \.scope) { r in
            let scope = r.scope == "global" ? LoadBalancingText.kubeVipGlobal : r.scope
            Text(verbatim: "\(scope): \(r.cidrs.joined(separator: ", "))").font(.caption.monospaced())
        }
        ForEach(vip.leases, id: \.self) { l in
            let age = checkupAge(l.renewed, now: Int64(Date().timeIntervalSince1970 * 1000))
            Text(
                verbatim: age.isEmpty
                    ? LoadBalancingText.kubeVipLease(l.name, l.holder)
                    : LoadBalancingText.kubeVipLeaseRenewed(l.name, l.holder, age)
            )
            .font(.caption).foregroundStyle(.secondary)
        }
    }
}

private struct ServiceLine: View {
    let service: LBService

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(alignment: .firstTextBaseline) {
                Text(verbatim: service.id).font(.callout.monospaced()).lineLimit(1).truncationMode(.middle)
                Spacer()
                if service.pending {
                    Text("No address").font(.caption).foregroundStyle(lbLevelColor(service.level))
                } else {
                    Text(verbatim: service.addresses.joined(separator: ", ")).font(.caption.monospaced())
                }
            }
            let notes = [
                service.pool.isEmpty ? "" : LoadBalancingText.servicePool(service.pool),
                service.announcedBy.isEmpty ? "" : LoadBalancingText.serviceAnnouncedBy(service.announcedBy),
            ]
            .filter { !$0.isEmpty }
            if !notes.isEmpty {
                Text(verbatim: notes.joined(separator: " · ")).font(.caption).foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .combine)
    }
}
