import SwiftUI
import IchorCore

/// "Add a node…": a machine booted from the Talos ISO waits in maintenance mode; type the address
/// its console shows and see its version, disks and interfaces; then make its config from a
/// member's (JoinConfigSection); then send it and follow the node until it joins (JoinApplySection).
struct JoinNodeView: View {
    @Binding var path: [Route]
    @Environment(AppModel.self) private var model
    @State private var address = ""
    @State private var inspection: LoadState<MaintenanceInspection>?
    @State private var request = 0
    @State private var configured: JoinRequest?

    var body: some View {
        List {
            Section {
                NavigationLink(value: Route.bootMedia) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("No machine yet? Boot a new machine")
                        Text(
                            "The booter command, the ISO and the iPXE link; the machine then waits in maintenance mode"
                        )
                        .font(.footnote).foregroundStyle(.secondary)
                    }
                }
            }
            Section {
                Text(
                    "Boot the new machine from the Talos ISO: it waits in maintenance mode and its console shows its IP address. Your phone must be on the same network, or on a VPN that reaches it."
                )
                .font(.footnote).foregroundStyle(.secondary)
                if model.activeSummary?.demo == true {
                    Text("In the demo, type demo to see a sample node.").note()
                }
                HStack {
                    TextField("Node address", text: $address, prompt: Text(verbatim: "192.168.1.42"))
                        .keyboardType(.URL)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .submitLabel(.go)
                        .onSubmit { Task { await inspect() } }
                    Button("Inspect") { Task { await inspect() } }
                        .disabled(joinAddress(address).isEmpty || isLoading)
                }
            } header: {
                Text("1. Inspect the new node")
            }
            if let inspection {
                switch inspection {
                case .loading:
                    Section { ProgressView().frame(maxWidth: .infinity) }
                case .failed(let error):
                    Section {
                        Text(verbatim: error).foregroundStyle(.statusBad)
                        Button("Retry") { Task { await inspect() } }
                    }
                case .loaded(let node, _, _):
                    inspectionSections(node)
                }
            }
            JoinConfigSection(disks: newNodeDisks, configured: $configured)
            JoinApplySection(request: joinRequest) { ref in
                path.removeLast()
                path.append(.node(ref))
            }
        }
        // A new address forgets the inspection: steps 2 and 3 only use an inspected node.
        .onChange(of: address) { _, _ in
            request += 1
            inspection = nil
        }
        .navigationTitle("Add a node")
        .clusterScreenTitle(Text("Add a node"))
        .navigationBarTitleDisplayMode(.inline)
    }

    /// Steps 1 and 2 done: a node in maintenance mode and a previewed config. The address is the
    /// one the node was inspected at, and the disk one of its own.
    private var joinRequest: JoinRequest? {
        guard case .loaded(let node, _, _) = inspection, node.maintenance, let c = configured,
            joinDiskFits(c.disk, node: node)
        else { return nil }
        return JoinRequest(
            address: node.address, template: c.template, role: c.role, hostname: c.hostname,
            disk: c.disk, staticAddress: c.staticAddress)
    }

    /// The writable disks of the inspected node, for step 2.
    private var newNodeDisks: [String] {
        if case .loaded(let node, _, _) = inspection, node.maintenance { return installDisks(node.disks) }
        return []
    }

    private var isLoading: Bool {
        if case .loading = inspection { return true }
        return false
    }

    @ViewBuilder
    private func inspectionSections(_ node: MaintenanceInspection) -> some View {
        if !node.maintenance {
            Section {
                StatusPill(label: String(localized: "Already installed"), color: .orange)
                Text(
                    "\(node.address) is already installed: it asks for a client certificate. Only a node booted from the ISO and not installed yet can be added here."
                )
            }
        } else {
            Section {
                HStack {
                    Text(verbatim: node.address).font(.headline.monospaced())
                    Spacer()
                    StatusPill(label: String(localized: "Maintenance mode"), color: .green)
                }
                Text(
                    verbatim: ["Talos \(node.version)", node.arch, node.platform].filter { !$0.isEmpty }.joined(
                        separator: "  ·  ")
                )
                .font(.caption).foregroundStyle(.secondary)
                SectionError(message: node.errors["system"])
                if let system = node.system {
                    if !system.manufacturer.isEmpty { LabeledContent("Manufacturer", value: system.manufacturer) }
                    if !system.product.isEmpty { LabeledContent("Product", value: system.product) }
                    if !system.serial.isEmpty { LabeledContent("Serial number", value: system.serial).monospaced() }
                }
            }
            Section("Disks") {
                SectionError(message: node.errors["disks"])
                if node.disks.isEmpty && node.errors["disks"] == nil {
                    Text("None").foregroundStyle(.secondary)
                }
                ForEach(node.disks) { disk in JoinDiskRow(disk: disk) }
            }
            Section("Interfaces") {
                SectionError(message: node.errors["links"])
                if node.links.isEmpty && node.errors["links"] == nil {
                    Text("None").foregroundStyle(.secondary)
                }
                ForEach(node.links) { link in
                    VStack(alignment: .leading, spacing: 3) {
                        LinkRow(link: link)
                        ForEach(node.addresses(on: link.name), id: \.self) { prefix in
                            Text(verbatim: prefix).font(.caption.monospaced())
                        }
                    }
                }
            }
        }
    }

    /// Only the latest inspection's answer is shown: an older, slower one is dropped.
    private func inspect() async {
        let typed = joinAddress(address)
        guard !typed.isEmpty else { return }
        request += 1
        let current = request
        inspection = .loading
        let loaded = await LoadState.from { try await TalosClient.maintenanceNodeInspect(address: typed) }
        if current == request { inspection = loaded }
    }
}

/// A disk of the node to join, as on the hardware screen: path, size, type and model.
private struct JoinDiskRow: View {
    let disk: DiskInfo

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack {
                Text(verbatim: disk.devPath.isEmpty ? disk.name : disk.devPath).font(.body.monospaced())
                if disk.systemDisk { StatusPill(label: String(localized: "System disk"), color: .blue) }
                Spacer()
                Text(verbatim: ByteCountFormatter.string(fromByteCount: Int64(clamping: disk.size), countStyle: .file))
                    .monospacedDigit()
            }
            Text(verbatim: [disk.type.uppercased(), disk.model].filter { !$0.isEmpty }.joined(separator: "  ·  "))
                .font(.caption).foregroundStyle(.secondary)
        }
    }
}
