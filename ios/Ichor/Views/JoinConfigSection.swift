import SwiftUI
import IchorCore

/// Step 2 of "Add a node…": a member's config made the new node's (same role, its own host name,
/// install disk and address), previewed with its secrets hidden. Sections of JoinNodeView's list.
struct JoinConfigSection: View {
    /// The new node's writable disks from step 1, offered as choices.
    let disks: [String]

    @Environment(AppModel.self) private var model
    @State private var members: LoadState<[NodeOverview]> = .loading
    @State private var template: NodeOverview?
    @State private var hostname = ""
    @State private var disk = ""
    @State private var address = ""
    @State private var preview: LoadState<JoinConfigPreview>?
    @State private var request = 0

    var body: some View {
        Section {
            Text("The new node gets the config of a member of the same role, with its own host name, install disk and address. The member is only read.")
                .font(.footnote).foregroundStyle(.secondary)
            switch members {
            case .loading:
                ProgressView().frame(maxWidth: .infinity)
            case .failed(let error):
                Text(verbatim: error).foregroundStyle(.statusBad)
                Button("Retry") { Task { await loadMembers() } }
            case .loaded(let nodes, _, _):
                templatePicker(nodes)
            }
        } header: {
            Text("2. Machine config from an existing node: role, host name and install disk")
        }
        .task { await loadMembers() }
        if let template {
            fields(template)
        }
        if let preview {
            previewSection(preview)
        }
    }

    @ViewBuilder
    private func templatePicker(_ nodes: [NodeOverview]) -> some View {
        let templates = joinTemplates(nodes)
        if templates.isEmpty {
            Text("No member answers: the new node's config is made from a member's.").note()
        } else {
            Picker("Template", selection: Binding(get: { template }, set: { pick($0, among: nodes) })) {
                Text(verbatim: "—").tag(NodeOverview?.none)
                ForEach(templates) { node in
                    Text(verbatim: "\(node.hostname.isEmpty ? node.node : node.hostname) · \(roleLabel(joinRole(node.role)))")
                        .tag(NodeOverview?.some(node))
                }
            }
        }
    }

    @ViewBuilder
    private func fields(_ template: NodeOverview) -> some View {
        Section {
            Text("Role: \(roleLabel(joinRole(template.role))) (the template's)")
                .font(.footnote).foregroundStyle(.secondary)
            TextField("Host name", text: edited($hostname))
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
            TextField("Install disk", text: edited($disk), prompt: Text(verbatim: "/dev/sda"))
                .font(.body.monospaced())
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
            if !disks.isEmpty {
                Picker("Install disk", selection: edited($disk)) {
                    if !disks.contains(disk) { Text(verbatim: disk.isEmpty ? "—" : disk).tag(disk) }
                    ForEach(disks, id: \.self) { path in Text(verbatim: path).tag(path) }
                }
            }
            VStack(alignment: .leading, spacing: 3) {
                TextField("Static address (optional)", text: edited($address), prompt: Text(verbatim: "192.168.1.50/24"))
                    .keyboardType(.numbersAndPunctuation)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                Text("With its prefix. Empty: the node takes its address from DHCP.")
                    .font(.caption).foregroundStyle(.secondary)
            }
            Button("Preview the config") { Task { await generate(template) } }
                .disabled(!complete || isLoading)
        }
    }

    @ViewBuilder
    private func previewSection(_ state: LoadState<JoinConfigPreview>) -> some View {
        Section {
            switch state {
            case .loading:
                ProgressView().frame(maxWidth: .infinity)
            case .failed(let error):
                Text(verbatim: error).foregroundStyle(.statusBad)
            case .loaded(let config, _, _):
                let size = ByteCountFormatter.string(fromByteCount: Int64(config.size), countStyle: .file)
                Text("Valid config for \(config.hostname) (\(size))").font(.headline)
                ForEach(config.warnings, id: \.self) { warning in
                    Text(verbatim: "• \(warning)").font(.footnote)
                }
                Text("The secrets are hidden here. The full config, with the cluster's secrets, only goes to the new node, in step 3.")
                    .font(.footnote).foregroundStyle(.secondary)
                ScrollView([.horizontal, .vertical]) {
                    Text(verbatim: config.redactedYaml)
                        .font(.caption.monospaced())
                        .textSelection(.enabled)
                        .fixedSize()
                }
                .frame(maxHeight: 360)
            }
        }
    }

    private var complete: Bool {
        template != nil && !hostname.trimmingCharacters(in: .whitespaces).isEmpty && !disk.trimmingCharacters(in: .whitespaces).isEmpty
    }

    private var isLoading: Bool {
        if case .loading = preview { return true }
        return false
    }

    /// `binding`, with any change making the shown preview stale.
    private func edited(_ binding: Binding<String>) -> Binding<String> {
        Binding(get: { binding.wrappedValue }, set: { value in
            binding.wrappedValue = value
            stale()
        })
    }

    /// Picks `node` as the template: the host name follows its role unless typed.
    private func pick(_ node: NodeOverview?, among nodes: [NodeOverview]) {
        let names = nodes.map(\.hostname)
        let suggested = template.map { suggestedHostname(role: $0.role, hostnames: names) }
        if let node, hostname.isEmpty || hostname == suggested {
            hostname = suggestedHostname(role: node.role, hostnames: names)
        }
        template = node
        stale()
    }

    private func stale() {
        request += 1
        preview = nil
    }

    private func loadMembers() async {
        guard let client = model.client else { return }
        members = .loading
        members = await LoadState.from { try await client.overview().nodes }
    }

    /// Only the latest preview's answer is shown: an older, slower one is dropped.
    private func generate(_ template: NodeOverview) async {
        guard let client = model.client else { return }
        request += 1
        let current = request
        preview = .loading
        let loaded = await LoadState.from {
            try await client.joinNodeConfigPreview(
                template: template.node, role: joinRole(template.role),
                hostname: hostname.trimmingCharacters(in: .whitespaces), installDisk: disk.trimmingCharacters(in: .whitespaces),
                staticAddress: address.trimmingCharacters(in: .whitespaces))
        }
        if current == request { preview = loaded }
    }

    private func roleLabel(_ role: String) -> String {
        role == "controlplane" ? String(localized: "Control plane") : String(localized: "Worker")
    }
}
