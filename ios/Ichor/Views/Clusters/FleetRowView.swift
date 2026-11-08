import IchorCore
import SwiftUI

/// One cluster of the fleet: its tone dot in the cluster's colour ring (`seed`, nil for the
/// demo's made-up fleet), its name, "2/3 ready", the critical and warning counts, the top
/// finding (the track in the app's language, the subject as the core wrote it) and the check's age.
struct FleetRowView: View {
    let row: FleetRow
    let seed: Int?
    let masked: Bool
    let now: Date

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            toneDot.padding(.top, 3)
            VStack(alignment: .leading, spacing: 4) {
                Text(verbatim: row.name).font(.headline).lineLimit(1)
                HStack(spacing: 8) {
                    if row.nodes > 0 {
                        Text("\(row.ready)/\(row.nodes) ready").font(.subheadline)
                    }
                    if row.critical > 0 { pill(Text("Critical: \(row.critical)"), .red) }
                    if row.warnings > 0 { pill(Text("Warnings: \(row.warnings)"), .orange) }
                }
                if let finding = row.topFinding {
                    Text(verbatim: findingText(finding))
                        .font(.subheadline)
                        .foregroundStyle(finding.critical ? .red : .orange)
                        .lineLimit(2)
                }
                Text(verbatim: checkedText).font(.caption).foregroundStyle(.secondary)
            }
            Spacer(minLength: 0)
        }
        .padding(.vertical, 2)
        .accessibilityElement(children: .combine)
    }

    private var toneDot: some View {
        Circle()
            .fill(toneColor)
            .frame(width: 10, height: 10)
            .padding(3)
            .overlay(Circle().stroke(seed.map(clusterAccent) ?? .secondary, lineWidth: 2))
            .accessibilityLabel(toneLabel)
    }

    private func pill(_ text: Text, _ color: Color) -> some View {
        text.font(.caption).padding(.horizontal, 8).padding(.vertical, 2)
            .background(color.opacity(0.15), in: Capsule())
            .foregroundStyle(color)
    }

    private var toneColor: Color {
        switch row.tone {
        case .bad: .red
        case .warn: .orange
        case .unknown: .secondary
        case .ok: .green
        }
    }

    private var toneLabel: Text {
        switch row.tone {
        case .bad: Text("Needs attention")
        case .warn: Text("Degraded")
        case .unknown: Text("Not checked")
        case .ok: Text("Fine")
        }
    }

    private func findingText(_ finding: FleetFinding) -> String { finding.text(masked: masked) }

    /// "Checked 12 minutes ago"; a row not checked lately says when it last was, or that it never was.
    private var checkedText: String {
        guard let date = row.checkedDate else { return String(localized: "Never checked") }
        let ago = RelativeDateTimeFormatter().localizedString(for: date, relativeTo: now)
        return row.stale ? String(localized: "Last checked \(ago)") : String(localized: "Checked \(ago)")
    }
}
