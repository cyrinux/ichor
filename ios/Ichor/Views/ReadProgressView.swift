import SwiftUI
import IchorCore

/// A slow read still loading, step by step (Go ReadProgress): a bar that fills as steps end,
/// "5 of 17 steps", then one row per step, with the count of a fan-out ("12 / 40") and how long
/// a step that took a while lasted. `stepLabel` names a step id.
struct ReadProgressView: View {
    let progress: ReadProgress
    let stepLabel: (String) -> String

    var body: some View {
        List {
            Section {
                VStack(alignment: .leading, spacing: 8) {
                    Text("Reading the cluster").font(.headline)
                    if let fraction = progress.fraction {
                        ProgressView(value: fraction)
                        Text("\(String(progress.done)) of \(String(progress.total)) steps")
                            .font(.footnote).foregroundStyle(.secondary).monospacedDigit()
                    } else {
                        ProgressView().frame(maxWidth: .infinity, alignment: .leading)
                    }
                }
                .padding(.vertical, 4)
            }
            Section {
                ForEach(progress.steps) { step in
                    ReadStepRow(step: step, label: stepLabel(step.id))
                }
            }
        }
        .themedBackground()
    }
}

private struct ReadStepRow: View {
    let step: ReadStep
    let label: String

    private var detail: String {
        switch step.state {
        case .running where step.total > 0: String(localized: "\(String(step.count)) / \(String(step.total))")
        case .done where step.ms >= 1000: String(localized: "\(String(step.ms / 1000)) s")
        default: ""
        }
    }

    private var stateText: String {
        switch step.state {
        case .done: String(localized: "Step done")
        case .running: String(localized: "Step in progress")
        case .failed: String(localized: "Step could not be read")
        case .pending: String(localized: "Step waiting")
        }
    }

    var body: some View {
        HStack(spacing: 12) {
            Group {
                switch step.state {
                case .done: Image(systemName: "checkmark.circle").foregroundStyle(.statusOK)
                case .failed: Image(systemName: "exclamationmark.triangle").foregroundStyle(.statusWarn)
                case .running: ProgressView().controlSize(.small)
                case .pending: Image(systemName: "circle").foregroundStyle(.secondary)
                }
            }
            .frame(width: 20)
            .accessibilityHidden(true)
            Text(verbatim: label).foregroundStyle(step.state == .pending ? .secondary : .primary)
            Spacer()
            if !detail.isEmpty {
                Text(verbatim: detail).font(.footnote).foregroundStyle(.secondary).monospacedDigit()
            }
        }
        // One spoken line per step: its name, its state, then the count or the duration.
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text(verbatim: [label, stateText, detail].filter { !$0.isEmpty }.joined(separator: ", ")))
    }
}
