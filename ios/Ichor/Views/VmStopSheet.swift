import SwiftUI
import IchorCore

/// A stop or restart to confirm by typing the VM's name; `force` skips the guest's grace period.
struct VmStopTarget: Identifiable, Hashable {
    let action: VmAction
    let force: Bool
    var id: String { "\(action.rawValue)\(force ? "-force" : "")" }
}

/// Stop or restart a VM: its name typed to confirm. A stop under runStrategy Always writes Halted
/// into the spec (said here); a forced one kills the instance without the grace period.
struct VmStopSheet: View {
    let vm: VirtualMachine
    let target: VmStopTarget
    let onConfirm: () -> Void

    @Environment(\.kubeActionAccess) private var access

    private var denial: KubeAccess? { access?.denial(for: target.action.kubeAction, in: vm.namespace) }

    private var message: String {
        var lines = [
            target.action == .stop
                ? String(
                    localized:
                        "The guest is asked to shut down, then the instance is deleted after its grace period. The VM keeps its disks."
                )
                : String(
                    localized:
                        "The instance is deleted and started again: the guest reboots and loses what is only in memory."
                )
        ]
        if target.force { lines.append(String(localized: "Force: no grace period")) }
        if target.action == .stop && vm.runStrategy == "Always" {
            lines.append(
                String(
                    localized:
                        "Its run strategy is Always: stopping sets it to Halted in the VM’s spec. A GitOps tool may set it back on its next sync."
                ))
        }
        if let denial { lines.append(denial.deniedText) }
        return lines.joined(separator: "\n\n")
    }

    var body: some View {
        HostnameConfirmationSheet(
            title: String(localized: "\(target.action.title) \(vm.name)?"), message: message, hostname: vm.name,
            actionTitle: target.action.title, enabled: denial == nil, onConfirm: onConfirm)
    }
}
