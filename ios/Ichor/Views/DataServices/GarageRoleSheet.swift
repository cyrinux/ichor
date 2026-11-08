import SwiftUI
import IchorCore

/// The role editor: the zone (the cluster's zones as suggestions), storage with a capacity or
/// gateway, and tags. Prefilled from the node's current or staged role, or, for a node joining,
/// from its disk and its Kubernetes node. Staging changes nothing until the layout is applied;
/// `onStaged` gets the layout with the staged change and its preview (os:admin).
struct GarageRoleSheet: View {
    let instance: GarageInstance
    let nodeID: String
    /// The layout already read by the caller, if any; else it is read here.
    let known: GarageLayout?
    let onStaged: (GarageLayout) -> Void

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var layout: GarageLayout?
    @State private var loadError: String?
    @State private var zone = ""
    @State private var gateway = false
    @State private var unit = GarageCapacityUnit.gib
    @State private var capacityText = ""
    @State private var tags: [String] = []
    @State private var newTag = ""
    @State private var saving = false
    @State private var failure: String?

    private var node: GarageLayoutNode? { layout?.node(nodeID) }
    private var current: GarageLayoutRole? { node?.effectiveRole }
    private var trimmedZone: String { zone.trimmingCharacters(in: .whitespaces) }
    private var zoneValid: Bool { !trimmedZone.isEmpty && trimmedZone.count <= 64 }
    private var capacity: Int64? { gateway ? 0 : garageCapacityBytes(capacityText, unit: unit) }
    private var capacityValid: Bool { gateway || (capacity ?? 0) >= garageMinCapacity }

    private var changed: Bool {
        guard let current else { return true }
        return trimmedZone != current.zone || gateway != current.gateway || (!gateway && capacity != current.capacity)
            || tags != current.tags
    }

    private var title: String {
        guard let node else { return String(localized: "Add \(String(nodeID.prefix(16))) to the layout") }
        return node.effectiveRole != nil
            ? String(localized: "Change the role of \(node.label)")
            : String(localized: "Add \(node.label) to the layout")
    }

    var body: some View {
        NavigationStack {
            Form {
                if let loadError {
                    Section { Text(verbatim: loadError).font(.footnote).foregroundStyle(.statusBad) }
                } else if layout == nil {
                    Section { ProgressView() }
                } else if let node {
                    form(node)
                } else {
                    Section {
                        Text("The cluster no longer lists this node.").font(.footnote).foregroundStyle(.statusBad)
                    }
                }
                if let failure {
                    Section { Text(verbatim: failure).font(.footnote).foregroundStyle(.statusBad) }
                }
            }
            .themedBackground()
            .navigationTitle(Text(verbatim: title))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    if saving {
                        ProgressView()
                    } else {
                        Button("Stage change") { Task { await stage() } }
                            .disabled(node == nil || !zoneValid || !capacityValid || !changed)
                    }
                }
            }
            .task { await load() }
        }
    }

    @ViewBuilder private func form(_ node: GarageLayoutNode) -> some View {
        Section {
            TextField("Zone", text: $zone)
                .autocorrectionDisabled()
                .textInputAutocapitalization(.never)
            if let layout, !layout.zones.isEmpty {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack {
                        ForEach(layout.zones, id: \.self) { z in
                            Button(z) { zone = z }
                                .buttonStyle(.bordered)
                                .tint(trimmedZone == z ? .accentColor : .secondary)
                        }
                    }
                }
            }
        } header: {
            Text("Zone")
        } footer: {
            if zoneValid {
                Text("Nodes of one zone share a failure domain: a site, a rack, a machine.")
            } else {
                Text("A zone is required (one line, 64 characters at most).")
            }
        }

        Section {
            Picker("Role", selection: $gateway) {
                Text("Storage").tag(false)
                Text("Gateway").tag(true)
            }
            .pickerStyle(.segmented)
            if gateway {
                Text("A gateway serves S3 requests and stores no data.").font(.footnote).foregroundStyle(.secondary)
            } else {
                HStack {
                    TextField("Capacity", text: $capacityText)
                        .keyboardType(.decimalPad)
                        .font(.body.monospaced())
                    Picker("Unit", selection: $unit) {
                        ForEach(GarageCapacityUnit.allCases, id: \.self) { Text(verbatim: $0.rawValue).tag($0) }
                    }
                    .labelsHidden()
                    .onChange(of: unit) { old, new in
                        // Keep the same bytes when the unit changes.
                        if let bytes = garageCapacityBytes(capacityText, unit: old) {
                            capacityText = garageCapacityText(bytes, unit: new)
                        }
                    }
                }
            }
        } header: {
            Text("Role")
        } footer: {
            if gateway {
                EmptyView()
            } else if !capacityValid {
                Text("Enter a positive number of at least 1 KiB.")
            } else if node.dataTotal > 0 {
                Text("Data disk \(formatBytes(node.dataTotal)); leave Garage less than the whole disk.")
            } else {
                Text("How much data Garage may store on this node.")
            }
        }

        Section {
            ForEach(tags, id: \.self) { tag in
                HStack {
                    Text(verbatim: tag).font(.body.monospaced())
                    Spacer()
                    Button {
                        tags.removeAll { $0 == tag }
                    } label: {
                        Image(systemName: "xmark.circle")
                    }
                    .buttonStyle(.borderless)
                    .accessibilityLabel(Text("Remove tag \(tag)"))
                }
            }
            HStack {
                TextField("Add a tag", text: $newTag)
                    .autocorrectionDisabled()
                    .textInputAutocapitalization(.never)
                    .onSubmit(addTag)
                if !newTag.trimmingCharacters(in: .whitespaces).isEmpty {
                    Button("Add", action: addTag).buttonStyle(.borderless)
                }
            }
        } header: {
            Text("Tags")
        } footer: {
            Text("Staging changes nothing yet: the layout then shows what applying would move.")
        }
    }

    private func addTag() {
        let tag = newTag.trimmingCharacters(in: .whitespaces)
        if !tag.isEmpty, !tags.contains(tag) { tags.append(tag) }
        newTag = ""
    }

    private func load() async {
        if let known {
            layout = known
        } else {
            guard let client = model.client else { return }
            do { layout = try await client.garageLayout(instance) } catch {
                loadError = error.localizedDescription
                return
            }
        }
        guard let node else { return }
        let role = node.effectiveRole
        zone = role?.zone ?? (layout?.zones.count == 1 ? layout?.zones[0] ?? "" : "")
        gateway = role?.gateway ?? false
        let bytes =
            (role?.gateway == false ? role?.capacity : nil) ?? garageSuggestedCapacity(dataTotal: node.dataTotal)
        unit = GarageCapacityUnit.forBytes(max(bytes, 1))
        capacityText = bytes > 0 ? garageCapacityText(bytes, unit: unit) : ""
        tags = role?.tags ?? (node.kubeNode.isEmpty ? [] : [node.kubeNode])
    }

    private func stage() async {
        guard let client = model.client, !saving else { return }
        saving = true
        defer { saving = false }
        do {
            let staged = try await client.garageStageRole(
                instance, node: nodeID, zone: trimmedZone, capacity: capacity ?? 0, gateway: gateway, tags: tags)
            onStaged(staged)
            dismiss()
        } catch {
            failure = error.localizedDescription
        }
    }
}
