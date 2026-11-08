import IchorCore
import SwiftUI

/// "Make a network for the node" or "Pick mine", above the network picker. The first makes (or
/// reuses) the ichor-nodes security group and virtual network in the resource group, then
/// `onReady` fills the pickers. Nothing runs until the person picks it: it writes at Azure. Rows
/// of AzureCreateSection's network section.
struct AzureGuidedNetworkRows: View {
    let resourceGroup: String
    let location: String
    let omni: Bool
    /// The location has no virtual network: the guided one is what to use.
    let noNetwork: Bool
    let credential: String
    let disabled: Bool
    let onReady: (AzureNetworkResult) -> Void

    @Environment(AppModel.self) private var model
    @State private var guided = false
    @State private var state: LoadState<AzureNetworkResult>?

    var body: some View {
        if noNetwork, !guided {
            Text("This location has no virtual network yet: Ichor can make one.")
                .font(.footnote).foregroundStyle(.secondary)
        }
        Picker(selection: $guided) {
            Text("Make a network for the node (recommended)").tag(true)
            Text("Pick mine").tag(false)
        } label: {
            EmptyView()
        }
        .pickerStyle(.inline)
        .labelsHidden()
        .disabled(disabled || isLoading || resourceGroup.isEmpty)
        .onChange(of: guided) { _, on in
            if on {
                Task { await run() }
            } else {
                state = nil
            }
        }
        .onChange(of: location) {
            guided = false
            state = nil
        }
        if guided {
            hint.font(.footnote).foregroundStyle(.secondary)
            stateRows
        }
    }

    private var hint: Text {
        if omni {
            return Text(
                "Ichor makes the security group and the virtual network ichor-nodes once in the resource group and reuses them. Nothing is opened inbound: Omni needs outbound access only, and the node gets a public IP."
            )
        }
        return Text(
            "Ichor makes the security group and the virtual network ichor-nodes once in the resource group and reuses them. Nothing is opened to the world; port 50000 is opened to this phone's address only. The node gets a public IP; a cluster elsewhere may need peering to reach it."
        )
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
            // The refusal names the missing action; the pickers below still work.
            Text(verbatim: error).foregroundStyle(.statusBad)
            Button("Retry") { Task { await run() } }
        case .loaded(let network, _, _)?:
            Group {
                if network.created.isEmpty {
                    Text("Reusing the network ichor-nodes.")
                } else {
                    Text("Made the security group and the virtual network ichor-nodes.")
                }
            }
            .font(.footnote)
            if !network.phoneIp.isEmpty {
                Text("TCP 50000 from this phone (\(network.phoneIp))").font(.footnote).foregroundStyle(.secondary)
            }
            ForEach(network.warnings, id: \.self) { warning in
                Text(verbatim: warning).font(.footnote).foregroundStyle(.orange)
            }
        }
    }

    private func run() async {
        guard let client = model.client else { return }
        let spec = AzureNetworkSpec(resourceGroup: resourceGroup, location: location, omni: omni)
        state = .loading
        let result = await LoadState.from { try await client.azureNetworkPrepare(credential: credential, spec: spec) }
        state = result
        if case .loaded(let network, _, _) = result { onReady(network) }
    }
}
