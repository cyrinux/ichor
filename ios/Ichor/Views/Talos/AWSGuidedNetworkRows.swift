import IchorCore
import SwiftUI

/// "Use a network Ichor sets up" or "Pick mine", above the subnet picker. The first makes (or
/// reuses) the ichor-nodes group in the default VPC, then `onReady` fills the pickers; the rules
/// it now has are listed. Nothing runs until the person picks it: it writes at AWS. Rows of
/// AWSCreateSection's subnet section.
struct AWSGuidedNetworkRows: View {
    let region: String
    let omni: Bool
    /// The new member's role; "" on Omni, where it is a worker until allocated.
    let role: String
    let credential: String
    let disabled: Bool
    let onReady: (AWSNetwork) -> Void

    @Environment(AppModel.self) private var model
    @State private var guided = false
    @State private var state: LoadState<AWSNetwork>?

    var body: some View {
        Picker(selection: $guided) {
            Text("Use a network Ichor sets up (recommended)").tag(true)
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
        .onChange(of: region) {
            guided = false
            state = nil
        }
        if guided {
            Text(
                "Ichor makes the security group ichor-nodes once in the default VPC and reuses it. Nothing is opened to the world; on plain Talos, port 50000 is opened to this phone's address only."
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
            // The refusal (no default VPC) says what to do at AWS; the pickers below still work.
            Text(verbatim: error).foregroundStyle(.statusBad)
            Button("Retry") { Task { await run() } }
        case .loaded(let network, _, _)?:
            let group = network.securityGroupId
            Group {
                if network.created {
                    Text("Made the security group \(group) in the default VPC.")
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

    private func ruleLabel(_ rule: AWSNetworkRule, phoneIp: String) -> Text {
        if rule.isSelf { return Text("All traffic from the group itself") }
        if rule.isPhone(phoneIp) { return Text("TCP 50000 from this phone (\(phoneIp))") }
        return Text(verbatim: rule.plain)
    }

    private func run() async {
        guard let client = model.client else { return }
        let spec = AWSNetworkSpec(region: region, role: role.isEmpty ? "worker" : role, omni: omni)
        state = .loading
        let result = await LoadState.from { try await client.awsNetworkPrepare(credential: credential, spec: spec) }
        state = result
        if case .loaded(let network, _, _) = result { onReady(network) }
    }
}
