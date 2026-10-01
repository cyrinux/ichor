import SwiftUI
import TalosViewerCore

/// Reboot mode / force shutdown options plus a confirmation that requires typing the hostname.
struct PowerSheet: View {
    let action: PowerAction
    let hostname: String
    let role: String
    let onConfirm: (PowerRequest) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var mode = RebootMode.default
    @State private var forceShutdown = false
    @State private var typed = ""

    init(action: PowerAction, hostname: String, role: String, onConfirm: @escaping (PowerRequest) -> Void) {
        self.action = action
        self.hostname = hostname
        self.role = role
        self.onConfirm = onConfirm
    }

    private var request: PowerRequest { PowerRequest(action: action, rebootMode: mode, forceShutdown: forceShutdown) }
    private var matches: Bool { typed.trimmingCharacters(in: .whitespaces) == hostname }

    var body: some View {
        NavigationStack {
            Form {
                switch action {
                case .reboot:
                    Section("Mode (talosctl reboot -m)") {
                        Picker("Mode", selection: $mode) {
                            ForEach(RebootMode.allCases) { mode in
                                VStack(alignment: .leading) {
                                    Text(mode.label)
                                    Text(mode.details).font(.caption).foregroundStyle(mode == .force ? .red : .secondary)
                                }
                                .tag(mode)
                            }
                        }
                        .pickerStyle(.inline)
                        .labelsHidden()
                    }
                case .shutdown:
                    Section {
                        Toggle(isOn: $forceShutdown) {
                            VStack(alignment: .leading) {
                                Text("Force (--force)")
                                Text("Skip the Kubernetes cordon/drain, e.g. when the API is down. Pods and services are still stopped.")
                                    .font(.caption).foregroundStyle(.secondary)
                            }
                        }
                    } footer: {
                        Text("It stays off until someone powers it on (Wake-on-LAN, IPMI or physically).")
                    }
                }
                if role == "controlplane" {
                    Section {
                        Text("Control-plane node: it leaves etcd while down. Make sure the other members are healthy, or the cluster can lose quorum.")
                            .foregroundStyle(.orange)
                    }
                }
                Section("Type \(hostname) to confirm") {
                    TextField(hostname, text: $typed)
                        .font(.body.monospaced())
                        .autocorrectionDisabled()
                        .textInputAutocapitalization(.never)
                }
                Section {
                    Button(role: .destructive) { onConfirm(request) } label: {
                        Text(request.title).fontWeight(request.forced ? .bold : .regular)
                    }
                    .disabled(!matches)
                }
            }
            .navigationTitle("\(action.title) \(hostname)?")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
            }
        }
    }
}
