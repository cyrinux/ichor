import IchorCore
import SwiftUI

/// "Make a Talos image": the architecture (the section's pick first), where the image goes, what
/// is created and deleted, and the cost line, then a plain confirmation: nothing of the cluster is
/// touched and the helper is deleted afterwards. The demo shows the sheet without a cost (it has
/// no key); its run is refused by the core.
struct ScalewayImageSheet: View {
    let zone: String
    let arches: [String]
    let template: String
    let credential: String
    let demo: Bool
    let onStart: (ScalewayImageSpec) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var arch: String
    @State private var cost: LoadState<ScalewayImageCost>?

    // Explicit: the private @State makes the memberwise init private.
    init(
        zone: String, arches: [String], arch: String, template: String, credential: String, demo: Bool,
        onStart: @escaping (ScalewayImageSpec) -> Void
    ) {
        self.zone = zone
        self.arches = arches
        self.template = template
        self.credential = credential
        self.demo = demo
        self.onStart = onStart
        _arch = State(initialValue: arch)
    }

    private var ready: Bool {
        if demo { return true }
        guard case .loaded = cost else { return false }
        return true
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Text(
                        "Ichor starts a throwaway Ubuntu Instance with an empty Block Storage volume and writes this cluster's Talos image to it. It then snapshots the volume, makes an image from the snapshot, and deletes the Instance, its volumes and its IP. The image and its snapshot are kept. Nothing of the cluster is touched. It takes about ten minutes."
                    )
                    .font(.footnote).foregroundStyle(.secondary)
                    Text("The image goes in the key's project, in zone \(zone).")
                        .font(.footnote).foregroundStyle(.secondary)
                    if arches.count > 1 {
                        Picker("Architecture", selection: $arch) {
                            ForEach(arches, id: \.self) { Text(verbatim: $0).tag($0) }
                        }
                    } else {
                        LabeledContent("Architecture") { Text(verbatim: arch) }
                    }
                }
                Section {
                    if demo {
                        Text("Demo cluster: these are samples, and nothing is created.")
                            .font(.footnote).foregroundStyle(.secondary)
                    } else if let cost {
                        costRow(cost)
                    }
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
                        onStart(ScalewayImageSpec(templateNode: template, arch: arch, zone: zone))
                        dismiss()
                    }
                    .disabled(!ready)
                }
            }
            .task(id: arch) { await loadCost() }
        }
    }

    @ViewBuilder
    private func costRow(_ state: LoadState<ScalewayImageCost>) -> some View {
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
                        "About \(value.minutes) minutes of \(value.type), then the \(value.snapshotGB) GB snapshot is billed by Scaleway per GB per month until you delete it. Price unavailable: see Scaleway's price list."
                    )
                } else {
                    let hourly = value.currency.isEmpty ? value.hourly : "\(value.hourly) \(value.currency)"
                    Text(
                        "About \(value.minutes) minutes of \(value.type) (\(hourly)/h), then the \(value.snapshotGB) GB snapshot is billed by Scaleway per GB per month until you delete it, before tax"
                    )
                }
            }
            .font(.footnote).foregroundStyle(.orange)
        }
    }

    private func loadCost() async {
        guard !demo else { return }
        let (arch, zone, credential) = (arch, zone, credential)
        cost = .loading
        cost = await LoadState.from {
            try await TalosClient.scalewayImageEstimate(credential: credential, arch: arch, zone: zone)
        }
    }
}
