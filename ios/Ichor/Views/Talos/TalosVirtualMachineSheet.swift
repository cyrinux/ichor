import SwiftUI
import IchorCore

/// One Talos VM: its state and the state it should be in, its stage and error, CPUs and memory,
/// addresses and interfaces, and its disks with their pool, size and phase. Read-only for now:
/// the power and console actions go in a section under the details.
struct TalosVirtualMachineSheet: View {
    let ref: TalosVMRef

    @Environment(\.dismiss) private var dismiss

    private var vm: TalosVM { ref.vm }

    var body: some View {
        NavigationStack {
            List {
                Section { header }
                details
                network
                if !vm.disks.isEmpty {
                    Section("Disks") {
                        ForEach(vm.disks, id: \.name) { DiskRow(disk: $0) }
                    }
                }
            }
            .themedBackground()
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } }
            }
        }
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(verbatim: vm.name).font(.headline.monospaced())
                Spacer()
                VmStateBadge(label: vm.stateLabel, state: vm.vmState)
            }
            Text(verbatim: ref.hostname).font(.caption).foregroundStyle(.secondary)
            if !vm.error.isEmpty {
                Text(verbatim: vm.error).font(.callout).foregroundStyle(.statusBad)
            }
        }
    }

    @ViewBuilder private var details: some View {
        Section {
            LabeledContent("State", value: vm.stateLabel)
            if !vm.wantedPowerState.isEmpty { LabeledContent("Wanted state", value: vm.wantedPowerState) }
            if !vm.stage.isEmpty {
                LabeledContent("Stage") {
                    Text(verbatim: vm.stage).foregroundStyle(vm.stage == "error" ? Color.statusBad : .secondary)
                }
            }
            if let resources = talosVMResources(vm) { LabeledContent("Resources", value: resources) }
            if !vm.profile.isEmpty { LabeledContent("Profile", value: vm.profile) }
            if !vm.addresses.isEmpty { LabeledContent("Addresses", value: vm.addresses.joined(separator: ", ")) }
        }
    }

    @ViewBuilder private var network: some View {
        if !vm.interfaces.isEmpty {
            Section("Network interfaces") {
                ForEach(Array(vm.interfaces.enumerated()), id: \.offset) { _, nic in
                    VStack(alignment: .leading, spacing: 2) {
                        LabeledContent {
                            Text(verbatim: nic.mac).font(.callout.monospaced())
                        } label: {
                            Text(verbatim: nic.name.isEmpty ? "-" : nic.name)
                        }
                        if !nic.addresses.isEmpty {
                            Text(verbatim: nic.addresses.joined(separator: ", ")).font(.caption).foregroundStyle(
                                .secondary)
                        }
                    }
                }
            }
        }
    }
}

/// "root  vm-pool · 40 GB · ready", its volume and format under it, its error in red.
private struct DiskRow: View {
    let disk: TalosVMDisk

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            let summary = [
                disk.pool.isEmpty ? nil : disk.pool, disk.sizeBytes > 0 ? formatBytes(disk.sizeBytes) : nil,
                disk.phase.isEmpty ? nil : disk.phase,
            ]
            .compactMap { $0 }
            LabeledContent {
                Text(verbatim: summary.joined(separator: " · ")).foregroundStyle(
                    disk.bad ? Color.statusBad : .secondary)
            } label: {
                Text(verbatim: disk.name)
            }
            .font(.callout.monospaced())
            let detail = [
                disk.volume.isEmpty ? nil : disk.volume, disk.format.isEmpty ? nil : disk.format,
                disk.readOnly ? String(localized: "read-only") : nil,
            ]
            .compactMap { $0 }
            if !detail.isEmpty {
                Text(verbatim: detail.joined(separator: " · ")).font(.caption).foregroundStyle(.secondary)
            }
            if !disk.error.isEmpty {
                Text(verbatim: disk.error).font(.caption).foregroundStyle(.statusBad)
            }
        }
    }
}
