import SwiftUI

/// How to create a dedicated talosconfig for the phone, with copyable commands per role.
struct HelpSheet: View {
    @Environment(\.dismiss) private var dismiss
    @State private var role = PhoneRole.reader

    enum PhoneRole: String, CaseIterable, Identifiable {
        case reader = "Read-only", operatorRole = "Operator", admin = "Admin"
        var id: String { rawValue }

        var talosRole: String {
            switch self {
            case .reader: "os:reader"
            case .operatorRole: "os:operator"
            case .admin: "os:admin"
            }
        }

        var unlocks: String {
            switch self {
            case .reader: "Monitoring only: overview, services, resources, logs, etcd, live graphs. Recommended for a phone."
            case .operatorRole: "Everything read-only, plus reboot and shutdown."
            case .admin: "Everything, including the cluster health check. Treat the phone like a laptop."
            }
        }
    }

    var body: some View {
        NavigationStack {
            List {
                Section {
                    Text("Generate a dedicated identity instead of copying your admin ~/.talos/config: if the phone is lost, its certificate only grants what you chose, and it expires on its own.")
                        .font(.callout)
                    Picker("Role", selection: $role) {
                        ForEach(PhoneRole.allCases) { Text($0.rawValue).tag($0) }
                    }
                    .pickerStyle(.segmented)
                    Text(role.unlocks).font(.footnote).foregroundStyle(.secondary)
                }
                Section("1. Sign a certificate on one control-plane node") {
                    CommandRow("talosctl -n <control-plane-ip> config new talosconfig-phone --roles \(role.talosRole) --crt-ttl 8760h")
                }
                Section("2. List every node the app should show") {
                    CommandRow("talosctl --talosconfig talosconfig-phone config node <node-1> <node-2> …")
                }
                Section("3. Bring it to the phone") {
                    Text("AirDrop it to Files and use File, or show it as a QR code and use QR code:").font(.footnote)
                    CommandRow("qrencode -t ansiutf8 -r talosconfig-phone")
                    Text("Delete other copies afterwards: the file contains the private key. The app encrypts it with a Secure Enclave key.")
                        .font(.footnote).foregroundStyle(.secondary)
                }
            }
            .navigationTitle("Create a talosconfig")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } } }
        }
    }
}

private struct CommandRow: View {
    let command: String
    init(_ command: String) { self.command = command }

    var body: some View {
        HStack {
            Text(command).font(.system(.footnote, design: .monospaced)).textSelection(.enabled)
            Spacer()
            Button {
                UIPasteboard.general.string = command
            } label: {
                Image(systemName: "doc.on.doc")
            }
            .buttonStyle(.borderless)
            .accessibilityLabel("Copy command")
        }
    }
}
