import IchorCore
import SwiftUI

/// The title of a Talos VM problem, as its background alert and the VM list word it.
func talosVMProblemTitle(kind: String, vm: String, host: String) -> String {
    switch kind {
    case TalosVMIssue.diskFailed: String(localized: "A disk of \(vm) failed on \(host)")
    case TalosVMIssue.error: String(localized: "\(vm) failed on \(host)")
    default: String(localized: "\(vm) is down on \(host)")
    }
}

/// The VMs that should run and do not, worded like their background alert, the node's own words
/// under each; one opens that VM's sheet. Shown at the top of the list, only when there is one.
struct TalosVMProblemsSection: View {
    let report: TalosClusterVMs
    let open: (TalosVMRef) -> Void

    var body: some View {
        let problems = report.problems
        if !problems.isEmpty {
            Section {
                ForEach(Array(problems.enumerated()), id: \.offset) { _, item in
                    row(item.entry, item.problem)
                }
            } header: {
                Text("Needs attention").foregroundStyle(.statusBad)
            }
        }
    }

    @ViewBuilder private func row(_ entry: TalosNodeVMsEntry, _ problem: TalosVMProblem) -> some View {
        let label = VStack(alignment: .leading, spacing: 2) {
            Text(verbatim: talosVMProblemTitle(kind: problem.kind, vm: problem.vm, host: entry.title))
                .font(.subheadline)
                .foregroundStyle(.statusBad)
            if !problem.detail.isEmpty {
                Text(verbatim: problem.detail).font(.caption).foregroundStyle(.secondary).lineLimit(3)
            }
        }
        if let vm = entry.report.vms.first(where: { $0.name == problem.vm }) {
            Button {
                open(TalosVMRef(node: entry.node, hostname: entry.title, vm: vm))
            } label: {
                label
            }
        } else {
            label
        }
    }
}
