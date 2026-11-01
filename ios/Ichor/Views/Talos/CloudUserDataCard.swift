import SwiftUI
import IchorCore
import UniformTypeIdentifiers

/// "Cloud host: user data": the text to paste as user data when the server is created at a cloud
/// provider, and the Talos cloud images to create it from. On Omni, Omni's join config, made on
/// open; on plain Talos, the role, host name and install disk of the new member first (Add a
/// node's fields, without the static address: clouds hand out addresses by DHCP). With `create`,
/// Ichor creates the server at Hetzner and makes the text itself: only the new member's fields and
/// the Hetzner section. Sections of BootMediaView's list.
struct CloudUserDataCard: View {
    let omni: Bool
    /// On Omni, the labels to sign into the join token.
    let omniLabels: String
    let create: Bool
    /// The provider whose Talos image to list; nil lists them all.
    let imagePlatform: String?
    @Binding var path: [Route]

    @Environment(AppModel.self) private var model
    @State private var members: LoadState<[NodeOverview]> = .loading
    @State private var template: NodeOverview?
    @State private var hostname = ""
    @State private var disk = ""
    @State private var data: LoadState<CloudUserData>?
    @State private var request = 0
    @State private var expanded = false
    /// Plain Talos: the secrets confirmation was given for the shown text.
    @State private var confirmed = false
    @State private var pending: Action?
    @State private var sharing = false

    private enum Action { case copy, share }

    // Explicit: the private @State makes the memberwise init private.
    init(omni: Bool, omniLabels: String, create: Bool, imagePlatform: String?, path: Binding<[Route]>) {
        self.omni = omni
        self.imagePlatform = imagePlatform
        self.create = create
        self.omniLabels = omniLabels
        _path = path
    }

    var body: some View {
        if create {
            if !omni {
                Section {
                    form
                } header: {
                    Text("The new node")
                }
                .task { if case .loading = members { await loadMembers() } }
            }
            HetznerCreateSection(
                omni: omni, template: template?.node ?? "", role: template.map { joinRole($0.role) } ?? "",
                hostname: hostname.trimmingCharacters(in: .whitespaces),
                disk: disk.trimmingCharacters(in: .whitespaces), path: $path)
        } else {
            pasteSections
        }
    }

    @ViewBuilder private var pasteSections: some View {
        Section {
            Group {
                if omni {
                    Text(
                        "Omni's join config: create the server from a Talos cloud image below with this text as its user data, and the machine shows up among Omni's machines, not yet allocated."
                    )
                } else {
                    Text(
                        "Create the server from a Talos cloud image below and paste this text as its user data: Talos reads it at first boot and joins the cluster."
                    )
                }
            }
            .font(.footnote).foregroundStyle(.secondary)
            if !omni { form }
        } header: {
            Text("Cloud host: user data")
        }
        .task(id: omniLabels) {
            if omni {
                await generate()
            } else if case .loading = members {
                await loadMembers()
            }
        }
        if let data {
            resultSection(data)
        }
        if case .loaded(let value, _, _) = data, !shownImages(value).isEmpty {
            BootMediaCard(
                title: String(localized: "Talos cloud images"),
                values: shownImages(value).map { BootValue(label: $0.providerLabel, text: $0.url) },
                hint: String(
                    localized:
                        "Talos \(value.talosVersion), \(shownImages(value)[0].arch): create the server, or the snapshot it starts from, from your provider's image."
                )
            )
        }
    }

    @ViewBuilder
    private var form: some View {
        switch members {
        case .loading:
            ProgressView().frame(maxWidth: .infinity)
        case .failed(let error):
            Text(verbatim: error).foregroundStyle(.statusBad)
            Button("Retry") { Task { await loadMembers() } }
        case .loaded(let nodes, _, _):
            let templates = joinTemplates(nodes)
            if templates.isEmpty {
                Text("No member answers: the new node's config is made from a member's.").note()
            } else {
                Picker("Template", selection: Binding(get: { template }, set: { pick($0, among: nodes) })) {
                    Text(verbatim: "—").tag(NodeOverview?.none)
                    ForEach(templates) { node in
                        Text(verbatim: "\(node.hostname.isEmpty ? node.node : node.hostname) · \(roleLabel(node.role))")
                            .tag(NodeOverview?.some(node))
                    }
                }
            }
            if let template {
                Text("Role: \(roleLabel(template.role)) (the template's)")
                    .font(.footnote).foregroundStyle(.secondary)
                TextField("Host name", text: edited($hostname))
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                TextField("Install disk", text: edited($disk), prompt: Text(verbatim: "/dev/sda"))
                    .font(.body.monospaced())
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                if !create {
                    Button("Make the user data") { Task { await generate() } }
                        .disabled(!complete || isLoading)
                }
            }
        }
    }

    @ViewBuilder
    private func resultSection(_ state: LoadState<CloudUserData>) -> some View {
        Section {
            switch state {
            case .loading:
                ProgressView().frame(maxWidth: .infinity)
            case .failed(let error):
                Text(verbatim: error).foregroundStyle(.statusBad)
                Button("Retry") { Task { await generate() } }
            case .loaded(let value, _, _):
                let size = ByteCountFormatter.string(fromByteCount: Int64(value.size), countStyle: .file)
                let limit = ByteCountFormatter.string(fromByteCount: Int64(value.limit), countStyle: .file)
                Text("\(size) of the \(limit) most clouds accept")
                    .font(.headline)
                    .foregroundStyle(value.overLimit ? Color.orange : Color.primary)
                ForEach(value.warnings, id: \.self) { warning in
                    Label {
                        Text(verbatim: warning)
                    } icon: {
                        Image(systemName: "info.circle")
                    }
                    .font(.footnote).foregroundStyle(.secondary)
                }
                if value.isOmni {
                    Text("Contains Omni's join token: share it only with people who may add machines.")
                        .font(.footnote).foregroundStyle(.orange)
                }
                Button {
                    expanded.toggle()
                } label: {
                    if expanded { Text("Hide the text") } else { Text("Show the text, secrets hidden") }
                }
                if expanded {
                    ScrollView([.horizontal, .vertical]) {
                        Text(verbatim: value.redactedPreview)
                            .font(.caption.monospaced())
                            .textSelection(.enabled)
                            .fixedSize()
                    }
                    .frame(maxHeight: 360)
                }
                Text("Too long for a QR code: copy or share it.").font(.footnote).foregroundStyle(.secondary)
                actions(value)
            }
        }
    }

    /// Copy and Share the full text; on plain Talos the first one asks to confirm the secrets go out.
    private func actions(_ value: CloudUserData) -> some View {
        HStack(spacing: 20) {
            Spacer()
            Button {
                guarded(.copy, value)
            } label: {
                Label("Copy", systemImage: "doc.on.doc")
            }
            Button {
                guarded(.share, value)
            } label: {
                Label("Share", systemImage: "square.and.arrow.up")
            }
        }
        .buttonStyle(.borderless)
        .alert(
            "Copy the cluster's secrets?",
            isPresented: Binding(get: { pending != nil }, set: { if !$0 { pending = nil } })
        ) {
            Button("Continue") {
                confirmed = true
                if let action = pending { run(action, value) }
                pending = nil
            }
            Button("Cancel", role: .cancel) { pending = nil }
        } message: {
            Text(
                "This text carries the cluster's secrets (its CA, tokens and keys): whoever holds it can join a node to the cluster. Paste it only into your cloud provider's user data field."
            )
        }
        .sheet(isPresented: $sharing) { ActivityView(items: [value.userData]) }
    }

    private func guarded(_ action: Action, _ value: CloudUserData) {
        if value.isOmni || confirmed { run(action, value) } else { pending = action }
    }

    /// Device-only pasteboard, expiring after two minutes: the text is a credential.
    private func run(_ action: Action, _ value: CloudUserData) {
        switch action {
        case .copy:
            UIPasteboard.general.setItems(
                [[UTType.utf8PlainText.identifier: value.userData]],
                options: [.localOnly: true, .expirationDate: Date().addingTimeInterval(120)])
        case .share:
            sharing = true
        }
    }

    private func shownImages(_ value: CloudUserData) -> [CloudImage] {
        value.images.filter { imagePlatform == nil || $0.platform == imagePlatform }
    }

    private var complete: Bool {
        template != nil && !hostname.trimmingCharacters(in: .whitespaces).isEmpty
            && !disk.trimmingCharacters(in: .whitespaces).isEmpty
    }

    private var isLoading: Bool {
        if case .loading = data { return true }
        return false
    }

    /// `binding`, with any change making the shown text stale.
    private func edited(_ binding: Binding<String>) -> Binding<String> {
        Binding(
            get: { binding.wrappedValue },
            set: { value in
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
        data = nil
        confirmed = false
    }

    private func loadMembers() async {
        guard let client = model.client else { return }
        members = .loading
        members = await LoadState.from { try await client.overview().nodes }
    }

    /// Only the latest answer is shown: an older, slower one is dropped.
    private func generate() async {
        guard let client = model.client else { return }
        request += 1
        let current = request
        confirmed = false
        data = .loading
        let loaded = await LoadState.from {
            if omni {
                try await client.nodeCloudUserDataOmni(labelsJSON: omniLabels)
            } else {
                try await client.nodeCloudUserData(
                    template: template?.node ?? "", role: template.map { joinRole($0.role) } ?? "",
                    hostname: hostname.trimmingCharacters(in: .whitespaces),
                    installDisk: disk.trimmingCharacters(in: .whitespaces))
            }
        }
        guard current == request else { return }
        data = loaded
    }

    private func roleLabel(_ role: String) -> String {
        joinRole(role) == "controlplane" ? String(localized: "Control plane") : String(localized: "Worker")
    }
}
