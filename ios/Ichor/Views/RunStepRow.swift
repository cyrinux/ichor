import IchorCore
import SwiftUI

/// A row of a followed run's timeline (an upgrade, a node maintenance): the phase, when it was
/// reached and its latest message.
struct RunStepRow: View {
    let label: String
    let state: UpgradeStepState
    /// Unix ms the phase was first reported, 0 when not reached.
    let at: Int64
    let message: String

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            icon.frame(width: 20)
            VStack(alignment: .leading, spacing: 2) {
                HStack {
                    Text(label)
                        .fontWeight(state == .current ? .semibold : .regular)
                        .foregroundStyle(state == .pending ? Color.secondary : Color.primary)
                    Spacer()
                    if at > 0 {
                        Text(Date(epochMillis: at), format: .dateTime.hour().minute().second())
                            .font(.caption.monospacedDigit()).foregroundStyle(.secondary)
                    }
                }
                if !message.isEmpty {
                    Text(verbatim: message).font(.caption)
                        .foregroundStyle(state == .failed ? Color.statusBad : Color.secondary)
                }
            }
        }
    }

    @ViewBuilder private var icon: some View {
        switch state {
        case .done: Image(systemName: "checkmark.circle.fill").foregroundStyle(.statusOK)
        case .current: ProgressView()
        case .failed: Image(systemName: "xmark.circle.fill").foregroundStyle(.statusBad)
        case .pending: Image(systemName: "circle").foregroundStyle(.secondary)
        }
    }
}
