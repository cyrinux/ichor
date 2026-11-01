import SwiftUI
import IchorCore

/// Where the new machine comes from: what BootMediaView then shows.
enum AddNodePath: String, CaseIterable, Identifiable, Hashable {
    case cloud, machine
    var id: String { rawValue }
}

/// A cloud provider, in alphabetical order; `platform` names its Talos image, nil lists them all.
enum CloudProvider: String, CaseIterable, Identifiable, Hashable {
    case hetzner, ovhcloud, scaleway, other
    var id: String { rawValue }

    var platform: String? {
        switch self {
        case .hetzner: "hcloud"
        case .ovhcloud: "openstack"
        case .scaleway: "scaleway"
        case .other: nil
        }
    }

    @ViewBuilder var label: some View {
        switch self {
        case .hetzner: Text(verbatim: "Hetzner")
        case .ovhcloud: Text(verbatim: "OVHcloud")
        case .scaleway: Text(verbatim: "Scaleway")
        case .other: Text("Another provider")
        }
    }
}

/// A row of "Where is the new machine?".
struct AddNodePathLabel: View {
    let way: AddNodePath

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            switch way {
            case .cloud:
                Text("A server at a cloud provider")
                Text("Pick your provider, and Ichor shows what it takes there.")
                    .font(.footnote).foregroundStyle(.secondary)
            case .machine:
                Text("A machine of your own")
                Text("Boot it with the booter command, a USB stick or iPXE.")
                    .font(.footnote).foregroundStyle(.secondary)
            }
        }
    }
}

/// "Boot a new machine…": the phone cannot serve a network boot (privileged ports), so it hands
/// over what does: the booter command for a laptop of the machine's network, the ISO, the iPXE
/// URL, on Omni the join arguments, and for a cloud host the user data to paste, or the server
/// Ichor creates where it can: only what the chosen way needs (nil asks). "The machine is up"
/// continues with Add a node.
struct BootMediaView: View {
    @Binding var path: [Route]
    @Environment(AppModel.self) private var model
    @State private var media: LoadState<BootMedia> = .loading
    /// "" until the first answer: the template member's architecture.
    @State private var arch = ""
    @State private var secureBoot = false
    @State private var request = 0
    /// On Omni, the labels to sign into the join token.
    @State private var omniLabels = omniLabelsJSON(role: "worker", extra: "")

    @State private var chosen: AddNodePath?
    @State private var provider: CloudProvider?

    private var omni: Bool { model.activeSummary?.omni == true }

    // Explicit: the private @State makes the memberwise init private.
    init(path: Binding<[Route]>, chosen: AddNodePath?) {
        _path = path
        _chosen = State(initialValue: chosen)
    }

    var body: some View {
        List {
            switch chosen {
            case nil: chooser
            case .cloud: cloudSections
            case .machine: machineSections
            }
            // The demo shows its samples there too, and refuses to allocate.
            if omni || model.activeSummary?.demo == true {
                Section {
                    Button("Allocate in Omni") { path.append(.omniAllocate) }
                }
            }
            if chosen != nil {
                Section {
                    Button("Choose another way") { chosen = nil }
                }
            }
        }
        .navigationTitle(title)
        .clusterScreenTitle(Text(title))
        .navigationBarTitleDisplayMode(.inline)
        .task(id: omniLabels) { await load() }
    }

    private var title: LocalizedStringKey { chosen == .machine ? "Boot a new machine" : "Add a node" }

    private var chooser: some View {
        Section {
            ForEach(AddNodePath.allCases) { way in
                Button {
                    chosen = way
                } label: {
                    AddNodePathLabel(way: way).foregroundStyle(.primary)
                }
            }
        } header: {
            Text("Where is the new machine?")
        }
    }

    @ViewBuilder private var cloudSections: some View {
        Section {
            Picker(selection: $provider) {
                Text(verbatim: "—").tag(CloudProvider?.none)
                ForEach(CloudProvider.allCases) { $0.label.tag(CloudProvider?.some($0)) }
            } label: {
                Text("Which provider?")
            }
        }
        switch provider {
        case nil: EmptyView()
        // Ichor can call these providers' APIs: it creates the machine itself.
        case .hetzner: hetznerSections
        case .ovhcloud: ovhSections
        // A server at a cloud provider boots its own image and reads the config as user data.
        case .some(let other):
            if omni { OmniLabelsSection(labelsJSON: $omniLabels) }
            CloudUserDataCard(
                omni: omni, omniLabels: omniLabels, create: nil, imagePlatform: other.platform, path: $path)
        }
    }

    @ViewBuilder private var hetznerSections: some View {
        Section {
            if omni {
                Text(
                    "1. Add your project's Read & Write API token (Hetzner console: Security, API tokens).\n2. Name the server, then pick a server type, a location and a Talos snapshot. No snapshot yet? Ichor can make one.\n3. Create the server: Ichor shows the cost first, then follows it until the machine appears in Omni.\n4. Allocate the machine to the cluster."
                )
            } else {
                Text(
                    "1. Pick a member to copy, then the new node's host name and install disk.\n2. Add your project's Read & Write API token (Hetzner console: Security, API tokens).\n3. Pick a server type, a location and a Talos snapshot. No snapshot yet? Ichor can make one.\n4. Create the server: Ichor shows the cost first, then follows it until the node joins."
                )
            }
        }
        CloudUserDataCard(omni: omni, omniLabels: omniLabels, create: .hetzner, imagePlatform: nil, path: $path)
    }

    @ViewBuilder private var ovhSections: some View {
        Section {
            if omni {
                Text(
                    "1. Add a service account with the IAM policy Ichor prints.\n2. Upload the Talos cloud image for OpenStack to the project once, from a computer.\n3. Name the instance, then pick a region, a flavor and the Talos image.\n4. Create it: Ichor shows the cost first, then follows it until the machine appears in Omni. Allocate it to the cluster."
                )
            } else {
                Text(
                    "1. Pick a member to copy, then the new node's host name and install disk.\n2. Add a service account with the IAM policy Ichor prints.\n3. Upload the Talos cloud image for OpenStack to the project once, from a computer.\n4. Pick a region, a flavor and the Talos image, then create it: Ichor shows the cost first and follows it until the node joins."
                )
            }
        }
        CloudUserDataCard(omni: omni, omniLabels: omniLabels, create: .ovhcloud, imagePlatform: nil, path: $path)
    }

    @ViewBuilder private var machineSections: some View {
        Section {
            Text(
                "Your phone cannot serve a network boot itself. Start the new machine with one of these: it then waits in maintenance mode, and Add a node takes it from there."
            )
            .font(.footnote).foregroundStyle(.secondary)
        }
        if omni { OmniLabelsSection(labelsJSON: $omniLabels) }
        switch media {
        case .loading:
            Section { ProgressView().frame(maxWidth: .infinity) }
        case .failed(let error):
            Section {
                Text(verbatim: error).foregroundStyle(.statusBad)
                Button("Retry") { Task { await load() } }
            }
        case .loaded(let value, _, _):
            targetSection(value)
            mediaSections(value)
        }
        // Add a node takes a machine in maintenance mode; Omni adds its machines itself.
        if let context = model.activeSummary, !context.omni {
            Section {
                Button("The machine is up: add it") { addNode() }
            }
        } else if omni {
            Section {
                Text("Omni adds the machine once it boots with these arguments").note()
            }
        }
    }

    @ViewBuilder
    private func targetSection(_ media: BootMedia) -> some View {
        Section {
            Text(verbatim: "Talos \(media.talosVersion)").font(.headline)
            VStack(alignment: .leading, spacing: 4) {
                Group {
                    if media.fromMember {
                        Text("Schematic of \(media.templateNode)")
                    } else {
                        Text("Image Factory default schematic, without extensions")
                    }
                }
                .font(.footnote).foregroundStyle(.secondary)
                Text(verbatim: media.schematic).font(.caption.monospaced()).textSelection(.enabled)
            }
            Picker("Architecture", selection: Binding(get: { arch }, set: { select(arch: $0) })) {
                ForEach(bootArchitectures, id: \.self) { Text(verbatim: $0).tag($0) }
            }
            .pickerStyle(.segmented)
            Toggle("Secure Boot", isOn: Binding(get: { secureBoot }, set: { set(secureBoot: $0) }))
            ForEach(media.warnings, id: \.self) { warning in
                Label {
                    Text(verbatim: warning)
                } icon: {
                    Image(systemName: "info.circle")
                }
                .font(.footnote).foregroundStyle(.secondary)
            }
        }
    }

    @ViewBuilder
    private func mediaSections(_ media: BootMedia) -> some View {
        BootMediaCard(
            title: String(localized: "Laptop on the machine's network: run booter"), values: media.booterValues,
            hint: String(
                localized:
                    "Serves the network boot (DHCP proxy and TFTP) from a Linux machine of the same network. Then power on the new machine with network boot."
            )
        )
        BootMediaCard(
            title: String(localized: "USB stick or virtual media: ISO"), values: [BootValue(label: "", text: media.iso)]
        )
        BootMediaCard(
            title: String(localized: "iPXE or UEFI HTTP boot: script URL"),
            values: [BootValue(label: "", text: media.ipxe)])
        BootMediaCard(
            title: String(localized: "Your own PXE server: kernel, initramfs, command line"),
            values: media.pxeFileValues)
        if let omni = media.omni {
            BootMediaCard(
                title: String(localized: "Omni: join the instance"), values: omni.values,
                hint: String(
                    localized: "Boots a machine that joins this Omni instance, then shows up among its machines."),
                sensitive: true
            )
        }
    }

    private func select(arch: String) {
        guard arch != self.arch else { return }
        self.arch = arch
        Task { await load() }
    }

    private func set(secureBoot: Bool) {
        self.secureBoot = secureBoot
        Task { await load() }
    }

    /// Back to Add a node when it opened this screen, else a new one.
    private func addNode() {
        if path.dropLast().last == .joinNode {
            path.removeLast()
        } else {
            path.append(.joinNode)
        }
    }

    /// Only the latest answer is shown: an older, slower one is dropped.
    private func load() async {
        guard let client = model.client else { return }
        request += 1
        let current = request
        media = .loading
        let loaded = await LoadState.from {
            if omni {
                try await client.nodeBootMediaOmni(arch: arch, secureBoot: secureBoot, labelsJSON: omniLabels)
            } else {
                try await client.nodeBootMedia(template: "", arch: arch, secureBoot: secureBoot)
            }
        }
        guard current == request else { return }
        media = loaded
        if case .loaded(let value, _, _) = loaded { arch = value.arch }
    }
}
