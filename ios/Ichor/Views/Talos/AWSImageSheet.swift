import IchorCore
import SwiftUI

/// "Make a Talos AMI": the architecture (that of the instance type asked for, editable), the
/// helper's subnet and security group (the section's picks, editable) and the cost line, then a
/// plain confirmation: nothing of the cluster is touched and the helper is terminated afterwards.
struct AWSImageSheet: View {
    let options: AWSOptions
    let credential: String
    let template: String
    let onStart: (AWSImageSpec) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var arch: String
    @State private var subnetId: String
    @State private var groupId: String
    @State private var cost: LoadState<AWSImageCost>?

    private static let architectures = ["amd64", "arm64"]

    // Explicit: the private @State makes the memberwise init private.
    init(
        options: AWSOptions, arch: String, subnetId: String, groupId: String, template: String, credential: String,
        onStart: @escaping (AWSImageSpec) -> Void
    ) {
        self.options = options
        self.template = template
        self.credential = credential
        self.onStart = onStart
        _arch = State(initialValue: arch.isEmpty ? Self.architectures[0] : arch)
        _subnetId = State(initialValue: subnetId)
        _groupId = State(initialValue: groupId)
    }

    private var subnet: AWSSubnet? { options.subnets.first { $0.id == subnetId } }

    /// The picked group if it is of the subnet's VPC, else "" (the VPC's default).
    private var chosenGroup: String {
        guard let subnet, options.securityGroups(in: subnet.vpcId).contains(where: { $0.id == groupId }) else {
            return ""
        }
        return groupId
    }

    private var ready: Bool {
        guard subnet != nil, case .loaded = cost else { return false }
        return true
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Text(
                        "Ichor starts a throwaway Amazon Linux instance that writes this cluster's Talos image to an extra volume, snapshots the volume and registers the AMI, then terminates the instance. Nothing of the cluster is touched. It takes about ten minutes."
                    )
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
            .navigationTitle("Make a Talos AMI")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Make the AMI") {
                        onStart(
                            AWSImageSpec(
                                templateNode: template, arch: arch, region: options.region, subnetId: subnetId,
                                securityGroupId: chosenGroup))
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
            Picker("Subnet of the throwaway instance", selection: $subnetId) {
                Text(verbatim: "—").tag("")
                ForEach(options.helperSubnets) { Text(verbatim: subnetName($0)).tag($0.id) }
            }
            if let subnet {
                Picker("Security group of the throwaway instance", selection: $groupId) {
                    Text("The VPC's default group").tag("")
                    ForEach(options.securityGroups(in: subnet.vpcId)) { group in
                        Text(verbatim: group.name.isEmpty ? group.id : group.name).tag(group.id)
                    }
                }
            }
        } footer: {
            Text("It downloads the image from the Image Factory: pick a subnet that assigns a public IP.")
        }
    }

    private func subnetName(_ subnet: AWSSubnet) -> String {
        let base = [subnet.name.isEmpty ? subnet.id : subnet.name, subnet.az, subnet.cidr]
            .filter { !$0.isEmpty }.joined(separator: " · ")
        return subnet.public ? base + " · " + String(localized: "assigns a public IP") : base
    }

    @ViewBuilder
    private func costRow(_ state: LoadState<AWSImageCost>) -> some View {
        switch state {
        case .loading:
            ProgressView().frame(maxWidth: .infinity)
        case .failed(let error):
            Text(verbatim: error).foregroundStyle(.statusBad)
            Button("Retry") { Task { await loadCost() } }
        case .loaded(let value, _, _):
            Group {
                if value.priced {
                    let hourly = "\(value.hourly) \(value.currency)"
                    let storage = "\(value.snapshotMonthlyPerGB) \(value.currency)"
                    Text(
                        "About \(value.minutes) minutes of \(value.instanceType) (\(hourly)/h), then \(storage) per GB per month for the snapshot until you delete it at AWS, before tax"
                    )
                } else {
                    Text(
                        "About \(value.minutes) minutes of \(value.instanceType), then snapshot storage per GB per month until you delete it at AWS. Prices unavailable: see AWS's EC2 and EBS pricing."
                    )
                }
            }
            .font(.footnote).foregroundStyle(.orange)
        }
    }

    private func loadCost() async {
        let (arch, region, credential) = (arch, options.region, credential)
        cost = .loading
        cost = await LoadState.from {
            try await TalosClient.awsImageEstimate(credential: credential, arch: arch, region: region)
        }
    }
}
