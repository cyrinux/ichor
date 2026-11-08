import IchorCore
import SwiftUI

/// "Make a Talos image": the architecture of the machine type picked in the section, where the
/// image goes and where the helper VM runs (the section's picks), and the cost line, then a plain
/// confirmation: nothing of the cluster is touched and the helper is deleted afterwards.
struct GCPImageSheet: View {
    let project: String
    let zone: String
    let arch: String
    let subnet: GCPSubnet
    let publicIp: Bool
    let template: String
    let credential: String
    let onStart: (GCPImageSpec) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var cost: LoadState<GCPImageEstimate>?

    // Explicit: the private @State makes the memberwise init private.
    init(
        project: String, zone: String, arch: String, subnet: GCPSubnet, publicIp: Bool, template: String,
        credential: String, onStart: @escaping (GCPImageSpec) -> Void
    ) {
        self.project = project
        self.zone = zone
        self.arch = arch
        self.subnet = subnet
        self.publicIp = publicIp
        self.template = template
        self.credential = credential
        self.onStart = onStart
    }

    private var ready: Bool {
        guard case .loaded = cost else { return false }
        return true
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Text(
                        "Ichor starts a throwaway Debian VM that writes this cluster's Talos image to an empty data disk, makes an image from the disk, then deletes the VM and the disk. The image is kept, labelled with the Talos version and schematic. Nothing of the cluster is touched. It takes about ten minutes."
                    )
                    .font(.footnote).foregroundStyle(.secondary)
                    let subnetName = subnet.name.isEmpty ? subnet.selfLink : subnet.name
                    Text(
                        "The image goes in project \(project). The helper VM runs in \(zone), on subnet \(subnetName)."
                    )
                    .font(.footnote).foregroundStyle(.secondary)
                    LabeledContent("Architecture") { Text(verbatim: arch) }
                } footer: {
                    networkFooter
                }
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
                            GCPImageSpec(
                                templateNode: template, arch: arch, project: project, zone: zone,
                                subnetwork: subnet.selfLink.isEmpty ? subnet.name : subnet.selfLink,
                                associatePublicIp: publicIp))
                        dismiss()
                    }
                    .disabled(!ready)
                }
            }
            .task { await loadCost() }
        }
    }

    // Literals in each branch: a ternary would make a String, shown unlocalized.
    @ViewBuilder private var networkFooter: some View {
        if publicIp {
            Text("The helper VM gets a public IP for a few minutes, to download the image from the Image Factory.")
        } else {
            Text(
                "The helper VM gets no public address: it downloads the image from the Image Factory through the subnet's Cloud NAT."
            )
        }
    }

    @ViewBuilder
    private func costRow(_ state: LoadState<GCPImageEstimate>) -> some View {
        switch state {
        case .loading:
            ProgressView().frame(maxWidth: .infinity)
        case .failed(let error):
            Text(verbatim: error).foregroundStyle(.statusBad)
            Button("Retry") { Task { await loadCost() } }
        case .loaded(let value, _, _):
            Group {
                if value.hourly.isEmpty || value.imageMonthlyPerGB.isEmpty {
                    Text(
                        "About \(value.minutes) minutes of \(value.machineType), then the image's storage per GB per month until you delete it at Google Cloud. Prices unavailable: see Google Cloud's Compute Engine pricing."
                    )
                } else {
                    let hourly = "\(value.hourly) \(value.currency)"
                    let monthly = "\(value.imageMonthlyPerGB) \(value.currency)"
                    Text(
                        "About \(value.minutes) minutes of \(value.machineType) (\(hourly)/h), then \(monthly) per GB per month for the image until you delete it at Google Cloud, before tax"
                    )
                }
            }
            .font(.footnote).foregroundStyle(.orange)
        }
    }

    private func loadCost() async {
        let (arch, zone, credential) = (arch, zone, credential)
        cost = .loading
        cost = await LoadState.from {
            try await TalosClient.gcpImageEstimate(credential: credential, arch: arch, zone: zone)
        }
    }
}
