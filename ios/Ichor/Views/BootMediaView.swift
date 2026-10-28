import SwiftUI
import IchorCore

/// "Boot a new machine…": the phone cannot serve a network boot (privileged ports), so it hands
/// over what does: the booter command for a laptop of the machine's network, the ISO, the iPXE
/// URL, and on Omni the join arguments. "The machine is up" continues with Add a node.
struct BootMediaView: View {
    @Binding var path: [Route]
    @Environment(AppModel.self) private var model
    @State private var media: LoadState<BootMedia> = .loading
    /// "" until the first answer: the template member's architecture.
    @State private var arch = ""
    @State private var secureBoot = false
    @State private var request = 0

    var body: some View {
        List {
            Section {
                Text("Your phone cannot serve a network boot itself. Start the new machine with one of these: it then waits in maintenance mode, and Add a node takes it from there.")
                    .font(.footnote).foregroundStyle(.secondary)
            }
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
            } else if model.activeSummary?.omni == true {
                Section {
                    Text("Omni adds the machine once it boots with these arguments").note()
                }
            }
        }
        .navigationTitle("Boot a new machine")
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
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
                Label { Text(verbatim: warning) } icon: { Image(systemName: "info.circle") }
                    .font(.footnote).foregroundStyle(.secondary)
            }
        }
    }

    @ViewBuilder
    private func mediaSections(_ media: BootMedia) -> some View {
        BootMediaCard(
            title: String(localized: "Laptop on the machine's network: run booter"), values: media.booterValues,
            hint: String(localized: "Serves the network boot (DHCP proxy and TFTP) from a Linux machine of the same network. Then power on the new machine with network boot.")
        )
        BootMediaCard(title: String(localized: "USB stick or virtual media: ISO"), values: [BootValue(label: "", text: media.iso)])
        BootMediaCard(title: String(localized: "iPXE or UEFI HTTP boot: script URL"), values: [BootValue(label: "", text: media.ipxe)])
        BootMediaCard(title: String(localized: "Your own PXE server: kernel, initramfs, command line"), values: media.pxeFileValues)
        if let omni = media.omni {
            BootMediaCard(
                title: String(localized: "Omni: join the instance"), values: omni.values,
                hint: String(localized: "Boots a machine that joins this Omni instance, then shows up among its machines."),
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
            try await client.nodeBootMedia(template: "", arch: arch, secureBoot: secureBoot)
        }
        guard current == request else { return }
        media = loaded
        if case .loaded(let value, _, _) = loaded { arch = value.arch }
    }
}
