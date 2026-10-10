import SwiftUI
import IchorCore

/// What a reset would wipe and leave, the `talosctl reset` options, then a confirmation that
/// requires typing the hostname; the button stays disabled while a blocker stands.
struct NodeResetSheet: View {
    struct Request {
        let wipe: ResetWipe
        let graceful: Bool
        let reboot: Bool
    }

    let node: String
    let hostname: String
    let onConfirm: (Request) -> Void

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var plan: LoadState<NodeResetPlan> = .loading
    @State private var wipe = ResetWipe.system
    @State private var graceful = true
    @State private var reboot = true
    @State private var typed = ""

    init(node: String, hostname: String, onConfirm: @escaping (Request) -> Void) {
        self.node = node
        self.hostname = hostname
        self.onConfirm = onConfirm
    }

    private var matches: Bool { typedConfirmationMatches(typed, token: hostname) }

    var body: some View {
        NavigationStack {
            LoadStateView(state: plan, retry: loadPlan) { plan in form(plan) }
                .navigationTitle(String(localized: "Reset \(hostname)?"))
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                }
        }
        .task { await loadPlan() }
    }

    private func form(_ plan: NodeResetPlan) -> some View {
        // A mode the plan does not offer (no user disk) falls back to the system disk.
        let chosen = plan.wipeModes.contains(wipe) ? wipe : .system
        return Form {
            Section {
                Text(planSummary(plan))
                ForEach(plan.blockers, id: \.self) { Text(verbatim: $0).foregroundStyle(.statusBad) }
                ForEach(plan.warnings, id: \.self) { Text(verbatim: $0).foregroundStyle(.statusWarn) }
            }
            if plan.allowed {
                options(plan, chosen: chosen)
                Section("Type \(hostname) to confirm") {
                    TextField("Hostname", text: $typed, prompt: Text(verbatim: hostname))
                        .font(.body.monospaced())
                        .autocorrectionDisabled()
                        .textInputAutocapitalization(.never)
                }
                Section {
                    Button(role: .destructive) {
                        onConfirm(Request(wipe: chosen, graceful: graceful, reboot: reboot))
                    } label: {
                        Text("Reset").fontWeight(.bold)
                    }
                    .disabled(!matches)
                }
            }
        }
    }

    @ViewBuilder
    private func options(_ plan: NodeResetPlan, chosen: ResetWipe) -> some View {
        Section("Wipe (talosctl reset --wipe-mode)") {
            Picker("Wipe", selection: Binding(get: { chosen }, set: { wipe = $0 })) {
                ForEach(plan.wipeModes, id: \.self) { mode in
                    VStack(alignment: .leading) {
                        Text(label(of: mode))
                        if mode != .system {
                            Text(verbatim: plan.userDisks.joined(separator: ", ")).font(.caption).foregroundStyle(.secondary)
                        }
                    }
                    .tag(mode)
                }
            }
            .pickerStyle(.inline)
            .labelsHidden()
        }
        Section {
            Toggle(isOn: $graceful) {
                VStack(alignment: .leading) {
                    Text("Leave etcd first (--graceful)")
                    Text("Cordon and drain the node and leave etcd cleanly before wiping.").font(.caption).foregroundStyle(.secondary)
                }
            }
        } footer: {
            if plan.leavesDeadMember(graceful: graceful) {
                Text("Its etcd member stays behind: remove it afterwards from the etcd screen (Remove member).")
                    .foregroundStyle(.statusWarn)
            }
        }
        Section {
            Toggle(isOn: $reboot) { Text("Reboot after (--reboot)") }
        } footer: {
            if reboot {
                Text("The node restarts once the reset is done.")
            } else {
                Text("It stays off until someone powers it on (Wake-on-LAN, IPMI or physically).")
            }
        }
    }

    private func planSummary(_ plan: NodeResetPlan) -> String {
        let role = plan.isControlPlane ? String(localized: "Control plane") : String(localized: "Worker")
        guard let member = plan.etcdMember else { return role }
        return role + " · " + String(localized: "etcd member \(member.id)")
    }

    private func label(of mode: ResetWipe) -> String {
        switch mode {
        case .all: String(localized: "Everything: the system disk and the user disks")
        case .system: String(localized: "System disk only: the node starts over as a new machine")
        case .user: String(localized: "User disks only")
        }
    }

    private func loadPlan() async {
        guard let client = model.client else { return }
        plan = await .from { try await client.resetPlan(node: node) }
    }
}
