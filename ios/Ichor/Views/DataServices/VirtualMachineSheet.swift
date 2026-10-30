import SwiftUI
import IchorCore

/// One VM: its status, resources, addresses, conditions, disks and migration, then the actions
/// its state allows, each also gated by what the credentials may do. Stop and restart ask for the
/// VM's name typed (VmStopSheet); migrate names the node it leaves. Console opens its serial
/// console (VMConsoleView).
struct VirtualMachineSheet: View {
    let vm: VirtualMachine
    let busy: Bool
    /// The confirmed action, and whether a stop skips the grace period.
    let onAction: (VmAction, Bool) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var typed: VmStopTarget?
    @State private var plain: VmAction?

    var body: some View {
        NavigationStack {
            List {
                Section { header }
                details
                if !vm.conditions.isEmpty {
                    Section("Conditions") {
                        ForEach(Array(vm.conditions.enumerated()), id: \.offset) { _, c in
                            VStack(alignment: .leading, spacing: 2) {
                                LabeledContent(
                                    c.type, value: [c.status, c.reason].filter { !$0.isEmpty }.joined(separator: " · ")
                                )
                                .foregroundStyle(c.bad ? Color.statusBad : .primary)
                                if !c.message.isEmpty {
                                    Text(verbatim: c.message).font(.caption).foregroundStyle(.secondary)
                                }
                            }
                        }
                    }
                }
                if !vm.disks.isEmpty {
                    Section("Disks") {
                        ForEach(vm.disks, id: \.name) { d in
                            LabeledContent(
                                d.name,
                                value: [d.phase, d.progress].filter { !$0.isEmpty && $0 != "N/A" }.joined(
                                    separator: " · ")
                            )
                            .font(.callout.monospaced())
                        }
                    }
                }
                if let m = vm.migration {
                    Section("Migration") {
                        LabeledContent(m.name, value: m.phase).font(.callout.monospaced())
                            .foregroundStyle(m.failed ? Color.statusBad : .primary)
                        Text(
                            String(
                                localized:
                                    "From \(m.sourceNode.isEmpty ? "?" : m.sourceNode) to \(m.targetNode.isEmpty ? "?" : m.targetNode)"
                            )
                        )
                        .font(.caption).foregroundStyle(.secondary)
                    }
                }
                VmActionsSection(vm: vm, busy: busy) { action, force in
                    if action == .stop || action == .restart {
                        typed = VmStopTarget(action: action, force: force)
                    } else {
                        plain = action
                    }
                }
                // Only a running instance has a console.
                Section("Serial console") {
                    NavigationLink("Console") { VMConsoleView(namespace: vm.namespace, name: vm.name) }
                        .disabled(vm.phase != "Running")
                        .kubeGated(.vmConsole, in: vm.namespace)
                    if vm.phase != "Running" {
                        Text("Start the VM first to open its console.").font(.caption).foregroundStyle(.secondary)
                    } else {
                        KubeDeniedNote(.vmConsole, in: vm.namespace)
                    }
                }
            }
            .themedBackground()
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } }
            }
            .sheet(item: $typed) { target in
                VmStopSheet(vm: vm, target: target) {
                    typed = nil
                    onAction(target.action, target.force)
                }
            }
            .confirmationDialog(
                plain.map { String(localized: "\($0.title) \(vm.name)?") } ?? "",
                isPresented: Binding(get: { plain != nil }, set: { if !$0 { plain = nil } }),
                titleVisibility: .visible, presenting: plain
            ) { action in
                Button(action.title) { onAction(action, false) }
                Button("Cancel", role: .cancel) {}
            } message: { action in
                Text(message(action))
            }
        }
        .loadsKubeActionAccess(namespace: vm.namespace)
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(verbatim: vm.name).font(.headline.monospaced())
                Spacer()
                VmStatusBadge(vm: vm)
            }
            Text(verbatim: vm.namespace).font(.caption).foregroundStyle(.secondary)
            if let progress = vmProgressText(vm.progress) {
                Text(verbatim: progress).font(.callout).foregroundStyle(.statusWarn)
            }
        }
    }

    @ViewBuilder private var details: some View {
        Section {
            LabeledContent("Run strategy", value: vm.runStrategy.isEmpty ? "-" : vm.runStrategy)
            if !vm.pendingChange.isEmpty { LabeledContent("Pending change", value: vm.pendingChange) }
            if vm.cpu > 0 || !vm.memory.isEmpty {
                LabeledContent(
                    "Resources",
                    value: [
                        vm.cpu > 0 ? String(localized: "\(vm.cpu) vCPU") : nil, vm.memory.isEmpty ? nil : vm.memory,
                    ]
                    .compactMap { $0 }.joined(separator: "  ·  "))
            }
            if !vm.node.isEmpty { LabeledContent("Node", value: vm.node) }
            if !vm.ips.isEmpty { LabeledContent("Addresses", value: vm.ips.joined(separator: ", ")) }
            if !vm.guestOS.isEmpty { LabeledContent("Guest OS", value: vm.guestOS) }
            if !vm.phase.isEmpty { LabeledContent("Instance phase", value: vm.phase) }
            if !vm.launcherImage.isEmpty {
                LabeledContent("Launcher image") {
                    Text(verbatim: vm.launcherImage).font(.caption.monospaced()).lineLimit(2)
                }
            }
        }
    }

    private func message(_ action: VmAction) -> String {
        switch action {
        case .migrate:
            String(
                localized:
                    "Live-migrates the running VM off \(vm.node.isEmpty ? "?" : vm.node) to a node the scheduler picks. The guest keeps running."
            )
        case .pause: String(localized: "The guest’s CPUs are frozen until it is unpaused; its memory stays.")
        default: String(localized: "KubeVirt applies it in a few seconds; the list follows the new status.")
        }
    }
}

/// The action buttons: enabled when the state allows and the credentials may; the refusal of an
/// allowed one is said under them.
private struct VmActionsSection: View {
    let vm: VirtualMachine
    let busy: Bool
    /// The action tapped, and whether it is the forced stop.
    let onTap: (VmAction, Bool) -> Void

    @Environment(\.kubeActionAccess) private var access

    /// Pause and unpause share one button: the state says which one it is.
    private var shown: [VmAction] { [.start, .stop, .restart, vm.allows(.unpause) ? .unpause : .pause, .migrate] }

    private func off(_ action: VmAction) -> Bool {
        busy || !vm.allows(action) || access?.denial(for: action.kubeAction, in: vm.namespace) != nil
    }

    var body: some View {
        Section("Actions") {
            ForEach(shown) { action in
                Button(action.title, role: action == .stop ? .destructive : nil) { onTap(action, false) }
                    .disabled(off(action))
            }
            // A hung guest: no grace period.
            Button("Force: no grace period", role: .destructive) { onTap(.stop, true) }
                .disabled(off(.stop))
            if let denial = shown.filter(vm.allows).lazy.compactMap({
                access?.denial(for: $0.kubeAction, in: vm.namespace)
            }).first {
                KubeDeniedLine(text: denial.deniedText)
            }
            if busy { Text("Action running…").font(.caption).foregroundStyle(.secondary) }
        }
    }
}
