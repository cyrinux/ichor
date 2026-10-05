import SwiftUI
import IchorCore

/// Wake-on-LAN from the node menus: sends magic packets, and opens the settings sheet. One
/// per app, so a menu (which cannot present anything itself) only has to set `editing`.
@Observable
@MainActor
final class WakeOnLanCenter {
    static let shared = WakeOnLanCenter()

    /// The node whose settings sheet is open.
    var editing: NodeOverview?
    /// How the last wake ended, until dismissed.
    var message: String?

    private init() {}

    /// Sends magic packets to every target of `node` (see wakeTargets), from the phone. Not tied
    /// to a screen: leaving it must not cut the packets short.
    func wake(_ node: NodeOverview, targets: [WolTarget]) {
        guard !targets.isEmpty else { return }
        Task {
            do {
                var destinations: [String] = []
                for target in targets {
                    let destination = try await WakeOnLanSender.send(target)
                    if !destinations.contains(destination) { destinations.append(destination) }
                }
                let via = destinations.joined(separator: ", ")
                message = String(localized: "Magic packet sent to \(node.hostname) (via \(via))")
            } catch {
                message = String(localized: "Could not send the magic packet: \(error.localizedDescription)")
            }
        }
    }
}

/// The Wake-on-LAN items of a node's menu; nothing in screenshot mode.
struct WakeOnLanMenuItems: View {
    let node: NodeOverview

    @Environment(AppModel.self) private var model

    var body: some View {
        if let fingerprint = model.wakeOnLanFingerprint {
            let targets = WakeOnLanStore.shared.wakeTargets(fingerprint: fingerprint, node: node.node)
            if !targets.isEmpty {
                Button { WakeOnLanCenter.shared.wake(node, targets: targets) } label: {
                    Label("Wake (Wake-on-LAN)", systemImage: "power.circle")
                }
            }
            Button { WakeOnLanCenter.shared.editing = node } label: {
                Label("Wake-on-LAN…", systemImage: "cable.connector")
            }
        }
    }
}

extension View {
    /// Shows the Wake-on-LAN settings sheet and the outcome of a wake, for the node menus below.
    func wakeOnLanPresenter() -> some View { modifier(WakeOnLanPresenter()) }
}

private struct WakeOnLanPresenter: ViewModifier {
    @Environment(AppModel.self) private var model

    func body(content: Content) -> some View {
        @Bindable var center = WakeOnLanCenter.shared
        content
            .sheet(item: $center.editing) { node in
                if let fingerprint = model.wakeOnLanFingerprint {
                    WakeOnLanSheet(node: node, fingerprint: fingerprint)
                }
            }
            .messageAlert($center.message)
            // Screenshot mode turned on with the sheet open: closed, not just emptied.
            .onChange(of: model.privacyMask) { _, masked in if masked { center.editing = nil } }
    }
}

/// How to wake `node`: its MAC address, and where to send the magic packet. The MACs of its
/// Ethernet links are offered: read live while it is up, else the ones it was last seen with;
/// a single one is filled in.
struct WakeOnLanSheet: View {
    let node: NodeOverview
    let fingerprint: String

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var mac = ""
    @State private var broadcast = ""
    @State private var port = ""
    @State private var error: WolInputError?
    @State private var live: [SeenMac] = []

    private var store: WakeOnLanStore { .shared }
    private var saved: WolTarget? { store.target(fingerprint: fingerprint, node: node.node) }
    private var links: [SeenMac] { live.isEmpty ? store.seenMacs(fingerprint: fingerprint, node: node.node) : live }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Text("Powers the node on by sending a magic packet from this phone. The phone must reach the node's network, directly or through a router that forwards the packet. Kept on this device only.")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                Section {
                    TextField("MAC address", text: $mac, prompt: Text(verbatim: "aa:bb:cc:dd:ee:ff"))
                        .font(.body.monospaced())
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .onChange(of: mac) { error = nil }
                    ForEach(links, id: \.self) { link in
                        Button {
                            mac = link.mac
                        } label: {
                            HStack {
                                Text(verbatim: "\(link.link)  \(link.mac)").font(.callout.monospaced())
                                Spacer()
                                if parseMac(mac) == parseMac(link.mac) { Image(systemName: "checkmark") }
                            }
                        }
                    }
                } header: {
                    Text("MAC address")
                } footer: {
                    if error == .mac { Text("Not a MAC address (e.g. aa:bb:cc:dd:ee:ff)").foregroundStyle(.statusBad) }
                }
                Section {
                    TextField("Broadcast address (optional)", text: $broadcast, prompt: Text(verbatim: wolDefaultBroadcast))
                        .keyboardType(.URL)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .onChange(of: broadcast) { error = nil }
                } header: {
                    Text("Broadcast address (optional)")
                } footer: {
                    if error == .broadcast {
                        Text("Not an IPv4 address or a host name").foregroundStyle(.statusBad)
                    } else {
                        Text("Empty: the broadcast address of the phone's Wi-Fi or Ethernet network")
                    }
                }
                Section {
                    TextField("UDP port (optional, 9 by default)", text: $port, prompt: Text(verbatim: "\(wolDefaultPort)"))
                        .keyboardType(.numberPad)
                        .onChange(of: port) { _, text in
                            let digits = String(text.filter(\.isASCII).filter(\.isNumber).prefix(5))
                            if digits != text { port = digits }
                            error = nil
                        }
                } header: {
                    Text("UDP port (optional, 9 by default)")
                } footer: {
                    if error == .port { Text("Port must be between 1 and 65535").foregroundStyle(.statusBad) }
                }
                if saved != nil {
                    Section {
                        Button("Forget", role: .destructive) {
                            store.set(nil, fingerprint: fingerprint, node: node.node)
                            dismiss()
                        }
                    }
                }
            }
            .themedBackground()
            .navigationTitle(String(localized: "Wake-on-LAN: \(node.hostname)"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) { Button("Save", action: save) }
            }
        }
        .onAppear(perform: fill)
        .task(id: node.node) { await readLive() }
    }

    private func fill() {
        guard let saved else { return }
        mac = saved.mac
        broadcast = saved.broadcast
        port = saved.port == wolDefaultPort ? "" : String(saved.port)
    }

    /// The node's MACs read now while it is up (and remembered on the way).
    private func readLive() async {
        if saved == nil && mac.isEmpty, let only = links.count == 1 ? links.first : nil { mac = only.mac }
        guard node.reachable, let client = model.client, let network = try? await client.network(node: node.node) else { return }
        let macs = seenMacs(network.links)
        live = macs
        store.record(macs, fingerprint: fingerprint, node: node.node)
        if saved == nil && mac.isEmpty, macs.count == 1 { mac = macs[0].mac }
    }

    private func save() {
        switch parseWolTarget(mac: mac, broadcast: broadcast, port: port) {
        case .success(let target):
            store.set(target, fingerprint: fingerprint, node: node.node)
            dismiss()
        case .failure(let problem):
            error = problem
        }
    }
}
