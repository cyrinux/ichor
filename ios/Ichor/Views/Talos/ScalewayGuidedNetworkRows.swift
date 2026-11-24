import IchorCore
import SwiftUI

/// "Make a security group for the nodes" or "Pick mine", above the security group picker. The
/// first makes (or reuses) the ichor-nodes group in `zone`, then `onReady` selects it; the rules it
/// now has are listed so the person can check them before creating. Nothing runs until the person
/// picks it: it writes at Scaleway. Rows of ScalewayCreateSection's form.
struct ScalewayGuidedNetworkRows: View {
    let zone: String
    let omni: Bool
    /// The new member's role; "" on Omni, where it is a worker until allocated.
    let role: String
    let credential: String
    let disabled: Bool
    let onReady: (ScalewayNetwork) -> Void

    @Environment(AppModel.self) private var model
    @State private var guided = false
    @State private var state: LoadState<ScalewayNetwork>?

    var body: some View {
        Picker(selection: $guided) {
            Text("Make a security group for the nodes (recommended)").tag(true)
            Text("Pick mine").tag(false)
        } label: {
            EmptyView()
        }
        .pickerStyle(.inline)
        .labelsHidden()
        .disabled(disabled || isLoading)
        .onChange(of: guided) { _, on in
            if on {
                Task { await run() }
            } else {
                state = nil
            }
        }
        .onChange(of: zone) {
            guided = false
            state = nil
        }
        if guided {
            Text(
                "Ichor makes the security group ichor-nodes once in this zone and reuses it. It drops what no rule lets in: every protocol from each cluster member's public address and, on plain Talos, TCP 50000 from this phone's address of the day. Nothing is opened to the world, and the group is kept."
            )
            .font(.footnote).foregroundStyle(.secondary)
            stateRows
        }
    }

    private var isLoading: Bool {
        if case .loading? = state { return true }
        return false
    }

    @ViewBuilder private var stateRows: some View {
        switch state {
        case nil:
            EmptyView()
        case .loading?:
            HStack {
                ProgressView()
                Text("Setting up the network…").foregroundStyle(.secondary)
            }
        case .failed(let error)?:
            // The refusal (no member with a public address, a key without the right) says what to
            // do; the picker below still works.
            Text(verbatim: error).foregroundStyle(.statusBad)
            Button("Retry") { Task { await run() } }
        case .loaded(let network, _, _)?:
            let group = network.name.isEmpty ? network.securityGroupId : network.name
            Group {
                if network.created {
                    Text("Made the security group \(group).")
                } else {
                    Text("Reusing the security group \(group).")
                }
            }
            .font(.footnote)
            ForEach(network.rules, id: \.self) { rule in
                ruleLabel(rule, phoneIp: network.phoneIp).font(.footnote).foregroundStyle(.secondary)
            }
            ForEach(network.warnings, id: \.self) { warning in
                Text(verbatim: warning).font(.footnote).foregroundStyle(.orange)
            }
        }
    }

    private func ruleLabel(_ rule: ScalewayNetworkRule, phoneIp: String) -> Text {
        if rule.isPhone(phoneIp) { return Text("TCP 50000 from this phone (\(phoneIp))") }
        if rule.isMember { return Text("All traffic from \(rule.description) (\(rule.source))") }
        return Text(verbatim: rule.plain)
    }

    private func run() async {
        guard let client = model.client else { return }
        let spec = ScalewayNetworkSpec(zone: zone, role: role.isEmpty ? "worker" : role, omni: omni)
        state = .loading
        let credential = self.credential
        let result = await LoadState.from {
            try await client.scalewayNetworkPrepare(credential: credential, spec: spec)
        }
        state = result
        if case .loaded(let network, _, _) = result { onReady(network) }
    }
}
