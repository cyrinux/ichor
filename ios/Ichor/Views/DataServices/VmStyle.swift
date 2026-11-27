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
    case .migrating(let from, let to):
        String(localized: "Migrating \(from.isEmpty ? "?" : from) → \(to.isEmpty ? "?" : to)")
    case nil: nil
    }
}

/// A VM's status as its platform writes it, coloured by state, read out with its meaning. Shared
/// by the KubeVirt screens and the Talos hypervisor's (Views/Talos).
struct VmStateBadge: View {
    let label: String
    let state: VmState

    var body: some View {
        let status = label.isEmpty ? "-" : label
        StatusPill(label: status, color: state.color)
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(String(localized: "Status: \(status)"))
    }
}

/// KubeVirt's status as written, coloured by state, read out with its meaning.
struct VmStatusBadge: View {
    let vm: VirtualMachine

    var body: some View {
        VmStateBadge(label: vm.status, state: vm.vmState)
    }
}

/// One count chip of a VM list: how many, its label, its colour.
struct VmCountChip {
    let count: Int
    let label: String
    let color: Color
}

/// The count chips of a VM list; only the non-zero ones, nothing when all are zero.
struct VmCountChips: View {
    let chips: [VmCountChip]

    var body: some View {
        let shown = chips.filter { $0.count > 0 }
        if !shown.isEmpty {
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 6) {
                    ForEach(shown, id: \.label) { StatusPill(label: $0.label, color: $0.color) }
                }
            }
        }
    }
}
