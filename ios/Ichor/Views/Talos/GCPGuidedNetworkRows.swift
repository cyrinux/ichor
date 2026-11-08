import IchorCore
import SwiftUI

/// "Use a network Ichor sets up" or "Pick mine", above the network picker. The first readies the
/// project's default network (its subnet of the zone's region and the firewall rules aimed at the
/// ichor-node tag), then `onReady` fills the pickers; the rules it now has are listed. Nothing runs
/// until the person picks it: it writes at Google Cloud. Rows of GCPCreateSection's network section.
struct GCPGuidedNetworkRows: View {
    let project: String
    let zone: String
    let omni: Bool
    /// The new member's role; "" on Omni, where it is a worker until allocated.
    let role: String
    let credential: String
    let disabled: Bool
    let onReady: (GCPNetworkResult) -> Void

    @Environment(AppModel.self) private var model
    @State private var guided = false
    @State private var state: LoadState<GCPNetworkResult>?

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
        .onChange(of: "\(project)/\(zone)") {
            guided = false
            state = nil
        }
        if guided {
            Text(
                "Ichor uses the project's default network and its subnet in this region, and adds two firewall rules once, aimed at the ichor-node tag of the nodes it makes: all traffic between Ichor nodes and, on plain Talos, TCP 50000 from this phone's address. Nothing is opened to the world, and the app never deletes the rules."
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
            // The refusal (no default network) says what to do at Google Cloud; the pickers below still work.
            Text(verbatim: error).foregroundStyle(.statusBad)
            Button("Retry") { Task { await run() } }
        case .loaded(let network, _, _)?:
            let added = (network.created + network.updated).joined(separator: ", ")
            Group {
                if added.isEmpty {
                    Text("The firewall rules were already there: nothing was added.")
                } else {
                    Text("Added the firewall rules: \(added).")
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

    private func ruleLabel(_ rule: GCPNetworkRule, phoneIp: String) -> Text {
        if rule.isSelf { return Text("All traffic between Ichor nodes") }
        if rule.isPhone(phoneIp) { return Text("TCP 50000 from this phone (\(phoneIp))") }
        return Text(verbatim: rule.plain)
    }

    private func run() async {
        guard let client = model.client else { return }
        let spec = GCPNetworkSpec(project: project, zone: zone, role: role.isEmpty ? "worker" : role, omni: omni)
        state = .loading
        let result = await LoadState.from { try await client.gcpNetworkPrepare(credential: credential, spec: spec) }
        state = result
        if case .loaded(let network, _, _) = result { onReady(network) }
    }
}
