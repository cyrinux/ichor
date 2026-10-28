import SwiftUI

/// The three ways to add a cluster, most common first: a cloud account, a kubeconfig, a
/// dedicated Talos identity (with copyable commands per role), then how to bring the file over.
struct HelpSheet: View {
    @Environment(\.dismiss) private var dismiss
    @State private var role = PhoneRole.reader

    enum PhoneRole: String, CaseIterable, Identifiable {
        case reader = "Read-only", operatorRole = "Operator", admin = "Admin"
        var id: String { rawValue }

        var label: String {
            switch self {
            case .reader: String(localized: "Read-only")
            case .operatorRole: String(localized: "Operator")
            case .admin: String(localized: "Admin")
            }
        }

        var talosRole: String {
            switch self {
            case .reader: "os:reader"
            case .operatorRole: "os:operator"
            case .admin: "os:admin"
            }
        }

        var unlocks: String {
            switch self {
            case .reader: String(localized: "Monitoring only: overview, services, resources, logs, etcd, live graphs. Recommended for a phone.")
            case .operatorRole: String(localized: "Everything read-only, plus reboot and shutdown.")
            case .admin: String(localized: "Everything, including the cluster health check. Treat the phone like a laptop.")
            }
        }
    }

    var body: some View {
        NavigationStack {
            List {
                cloudSection
                kubeconfigSection
                talosSections
                bringSection
            }
            .navigationTitle("How to add a cluster")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } } }
        }
    }

    /// One line per provider of the add screen, from the fields DiscoverClusters takes
    /// (go/ichorgo/kube_discover.go), then Sidero Omni's omniconfig.
    private var cloudSection: some View {
        Section("From a cloud account") {
            Text("Tap the provider on the add screen and sign in: the app lists the account's clusters and adds the ones you pick.")
                .font(.footnote)
            Text("EKS: an IAM Identity Center start URL and its region, or access keys.").font(.footnote)
            Text("GKE: your Google account, gcloud user credentials, a workforce login config or a service account key.").font(.footnote)
            Text("AKS: a service principal (tenant, subscription, client ID and secret).").font(.footnote)
            Text("DigitalOcean: an API token.").font(.footnote)
            Text("Rancher: the server address and an API key.").font(.footnote)
            Text("Sidero Omni: your omniconfig").font(.footnote.bold())
            Text("Import or scan your omniconfig.yaml, or the talosconfig Omni gives for your whole account: the Omni sign-in opens with the address and account filled in, then adds every cluster. These files hold no secret.")
                .font(.footnote)
            CommandRow("qrencode -t ansiutf8 -r ~/.config/omni/config")
        }
    }

    private var kubeconfigSection: some View {
        Section("From a kubeconfig") {
            Text("A kubeconfig with a client certificate or a token works: k3s, kubeadm, RKE2 or a managed cluster with a ServiceAccount. A cluster that signs in (OIDC, EKS, GKE, AKS, DigitalOcean, Rancher) asks for it once added.")
                .font(.footnote)
            Text("Keep only the cluster to add, with its certificates in the file:").font(.footnote)
            CommandRow("kubectl config view --flatten --minify > kubeconfig-phone")
            Text("Prefer a ServiceAccount with read-only rights over your admin credentials.")
                .font(.footnote).foregroundStyle(.secondary)
            NavigationLink("How to create a service account") { ServiceAccountHelpView() }
        }
    }

    @ViewBuilder private var talosSections: some View {
        Section("A Talos identity for this phone") {
            Text("Generate a dedicated identity instead of copying your admin ~/.talos/config: if the phone is lost, its certificate only grants what you chose, and it expires on its own.")
                .font(.callout)
            Picker("Role", selection: $role) {
                ForEach(PhoneRole.allCases) { Text($0.label).tag($0) }
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
    }

    /// A kubeconfig or a talosconfig: AirDrop to Files, or one or more QR codes.
    private var bringSection: some View {
        Section("Bring the file to the phone") {
            Text("AirDrop it to Files and use File, or show it as a QR code and use QR code:").font(.footnote)
            CommandRow("qrencode -t ansiutf8 -r <file>")
            Text("Too large for a QR code? Compress it, the app expands it:").font(.footnote)
            CommandRow("gzip -9c <file> | qrencode -8 -t ansiutf8")
            Text("Still too large? From the Ichor repository, split it over several codes and scan them in any order:").font(.footnote)
            CommandRow("just qr-parts <file>")
            Text("Delete other copies afterwards: the file contains the private key. The app encrypts it with a Secure Enclave key.")
                .font(.footnote).foregroundStyle(.secondary)
        }
    }
}

/// A monospace block of `command` (a command, or YAML) with a button that copies it.
struct CommandRow: View {
    let command: String
    let copyLabel: LocalizedStringKey

    init(_ command: String, copyLabel: LocalizedStringKey = "Copy command") {
        self.command = command
        self.copyLabel = copyLabel
    }

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
            .accessibilityLabel(copyLabel)
        }
    }
}
