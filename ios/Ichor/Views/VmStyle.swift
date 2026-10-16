import SwiftUI
import IchorCore

extension VmState {
    /// The badge colour: failing red, on its way amber, running green, stopped muted.
    var color: Color {
        switch self {
        case .failing: .statusBad
        case .changing: .statusWarn
        case .running: .statusOK
        case .stopped: .secondary
        }
    }
}

extension VmAction {
    /// The button and confirmation verb.
    var title: String {
        switch self {
        case .start: String(localized: "Start")
        case .stop: String(localized: "Stop")
        case .restart: String(localized: "Restart")
        case .migrate: String(localized: "Migrate")
        case .pause: String(localized: "Pause")
        case .unpause: String(localized: "Unpause")
        }
    }
}

/// "Importing win11-root 62.00%" or "Migrating worker-2 → worker-3", nil when nothing is in flight.
func vmProgressText(_ progress: VmProgress?) -> String? {
    switch progress {
    case .importing(let disk, let value): String(localized: "Importing \(disk) \(value.isEmpty ? "…" : value)")
    case .migrating(let from, let to): String(localized: "Migrating \(from.isEmpty ? "?" : from) → \(to.isEmpty ? "?" : to)")
    case nil: nil
    }
}

/// KubeVirt's status as written, coloured by state, read out with its meaning.
struct VmStatusBadge: View {
    let vm: VirtualMachine

    var body: some View {
        let status = vm.status.isEmpty ? "-" : vm.status
        StatusPill(label: status, color: vm.vmState.color)
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(String(localized: "Status: \(status)"))
    }
}
