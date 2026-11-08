import IchorCore
import SwiftUI

/// "Make a Talos image": the architecture (that of the size or image picked in the section,
/// editable), the helper VM's network, subnet and security group (the section's picks, editable)
/// and the cost line, then a plain confirmation: nothing of the cluster is touched and the helper
/// is deleted afterwards. The image goes in the section's resource group and location.
struct AzureImageSheet: View {
    let options: AzureOptions
    let resourceGroup: String
    let credential: String
    let template: String
    let onStart: (AzureImageSpec) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var arch: String
    @State private var networkId: String
    @State private var subnetId: String
    @State private var nsgId: String
    @State private var cost: LoadState<AzureImageEstimate>?

    private static let architectures = ["amd64", "arm64"]

    // Explicit: the private @State makes the memberwise init private.
    init(
        options: AzureOptions, resourceGroup: String, arch: String, networkId: String, subnetId: String,
        nsgId: String, template: String, credential: String, onStart: @escaping (AzureImageSpec) -> Void
    ) {
        self.options = options
        self.resourceGroup = resourceGroup
        self.template = template
        self.credential = credential
        self.onStart = onStart
        _arch = State(initialValue: arch.isEmpty ? Self.architectures[0] : arch)
        _networkId = State(initialValue: networkId)
        _subnetId = State(initialValue: subnetId)
        _nsgId = State(initialValue: nsgId)
    }

    private var subnet: AzureSubnet? { options.subnets(in: networkId).first { $0.id == subnetId } }

    private var ready: Bool {
        guard subnet != nil, case .loaded = cost else { return false }
        return true
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Text(
                        "Ichor starts a throwaway Ubuntu VM that writes this cluster's Talos image to an empty data disk, makes a managed image from the disk, then deletes the VM and the disk. Nothing of the cluster is touched. It takes about ten minutes."
                    )
                    .font(.footnote).foregroundStyle(.secondary)
                    let location = options.locationLabel(options.location)
                    Text("The image goes in resource group \(resourceGroup), in \(location).")
                        .font(.footnote).foregroundStyle(.secondary)
                    Picker("Architecture", selection: $arch) {
                        ForEach(Self.architectures, id: \.self) { Text(verbatim: $0).tag($0) }
                    }
                }
                networkSection
                if let cost {
                    Section { costRow(cost) }
                }
            }
            .navigationTitle("Make a Talos image")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Make the image") {
                        onStart(
                            AzureImageSpec(
                                templateNode: template, arch: arch, resourceGroup: resourceGroup,
                                location: options.location, subnetId: subnetId, securityGroupId: nsgId))
                        dismiss()
                    }
                    .disabled(!ready)
                }
            }
            .task(id: arch) { await loadCost() }
        }
    }

    @ViewBuilder private var networkSection: some View {
        Section {
            Picker("Virtual network", selection: $networkId) {
                Text(verbatim: "—").tag("")
                ForEach(options.networks) { Text(verbatim: "\($0.name) · \($0.resourceGroup)").tag($0.id) }
            }
            .onChange(of: networkId) { subnetId = "" }
            if !networkId.isEmpty {
                Picker("Subnet", selection: $subnetId) {
                    Text(verbatim: "—").tag("")
                    ForEach(options.subnets(in: networkId)) { Text(verbatim: $0.pickerLabel).tag($0.id) }
                }
            }
            if subnet != nil {
                Picker("Network security group", selection: $nsgId) {
                    Text(verbatim: "—").tag("")
                    ForEach(options.securityGroups) {
                        Text(verbatim: "\($0.name) · \($0.resourceGroup)").tag($0.id)
                    }
                }
            }
        } footer: {
            Text("The helper VM gets a public IP for a few minutes, to download the image from the Image Factory.")
        }
    }

    @ViewBuilder
    private func costRow(_ state: LoadState<AzureImageEstimate>) -> some View {
        switch state {
        case .loading:
            ProgressView().frame(maxWidth: .infinity)
        case .failed(let error):
            Text(verbatim: error).foregroundStyle(.statusBad)
            Button("Retry") { Task { await loadCost() } }
        case .loaded(let value, _, _):
            Group {
                if value.hourly.isEmpty {
                    Text(
                        "About \(value.minutes) minutes of \(value.size), then the image's storage per GB per month, billed as a managed disk until you delete it at Azure. Price unavailable: see Azure's VM and managed disk pricing."
                    )
                } else {
                    let hourly = "\(value.hourly) \(value.currency)"
                    Text(
                        "About \(value.minutes) minutes of \(value.size) (\(hourly)/h), then the image's storage per GB per month, billed as a managed disk until you delete it at Azure, before tax"
                    )
                }
            }
            .font(.footnote).foregroundStyle(.orange)
        }
    }

    private func loadCost() async {
        let (arch, location, credential) = (arch, options.location, credential)
        cost = .loading
        cost = await LoadState.from {
            try await TalosClient.azureImageEstimate(credential: credential, arch: arch, location: location)
        }
    }
}

extension AzureSubnet {
    /// "nodes · 10.0.0.0/24", then "NAT gateway" or "no NSG" when they apply.
    var pickerLabel: String {
        var parts = [name.isEmpty ? id : name]
        if !cidr.isEmpty { parts.append(cidr) }
        if natGateway { parts.append(String(localized: "NAT gateway")) }
        if nsgId.isEmpty { parts.append(String(localized: "no NSG")) }
        return parts.joined(separator: " · ")
    }
}
