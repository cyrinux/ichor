import SwiftUI
import IchorCore

/// One image or chart of the Updates screen: its name, "running → newest", who applied it, then a
/// chip per candidate (patch, minor, major: a tap opens that version's edit), the badge of what
/// already bumps it, or why it is not comparable. While selecting, a row that can be proposed has
/// a check (onToggle) and its chips pick the version it is ticked with (selectedVersion). Same
/// row as Android's UpdateRow.kt.
struct UpdateRowView: View {
    let row: UpdateRow
    let resolving: Bool
    /// Why a tap led nowhere.
    let note: String?
    var selecting = false
    var selectedVersion: String?
    var onToggle: () -> Void = {}
    let onPick: (String) -> Void

    private var selectable: Bool { selecting && row.editable && !row.candidates.isEmpty }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                if selectable {
                    Button(action: onToggle) {
                        Image(systemName: selectedVersion == nil ? "circle" : "checkmark.circle.fill")
                            .foregroundStyle(selectedVersion == nil ? Color.secondary : Color.accentColor)
                    }
                    .buttonStyle(.borderless)
                    .accessibilityLabel(Text(verbatim: row.name))
                    .accessibilityAddTraits(selectedVersion == nil ? [] : .isSelected)
                }
                Text(verbatim: row.name).font(.body.monospaced()).lineLimit(2)
                Spacer()
                if resolving { ProgressView() }
            }
            Group {
                if row.newest.isEmpty {
                    Text(verbatim: row.running)
                } else {
                    Text("\(row.running) → \(row.newest)")
                }
            }
            .font(.caption).foregroundStyle(.secondary)
            if let owner = row.owner, !owner.name.isEmpty {
                Text(verbatim: "\(owner.kind) \(owner.namespace)/\(owner.name)").font(.caption).foregroundStyle(
                    .secondary
                ).lineLimit(1)
            }
            if let automation = row.automation {
                StatusPill(label: automationLabel(automation), color: .accentColor)
            } else if !row.candidates.isEmpty {
                HStack(spacing: 8) {
                    ForEach(row.candidates, id: \.self) { candidate in
                        chip(candidate)
                    }
                }
            } else if !row.reason.isEmpty {
                Text(verbatim: row.reason).font(.caption).foregroundStyle(.secondary)
            }
            if let note, !note.isEmpty {
                Text(verbatim: note).font(.caption).foregroundStyle(.red)
            }
        }
        .padding(.vertical, 2)
    }

    /// A candidate's chip: opens its edit, or while selecting picks the version (filled when picked).
    @ViewBuilder private func chip(_ candidate: UpdateCandidate) -> some View {
        let button = Button {
            onPick(candidate.version)
        } label: {
            Text(chipLabel(candidate)).lineLimit(1)
        }
        .controlSize(.small)
        if selectable && candidate.version == selectedVersion {
            button.buttonStyle(.borderedProminent)
        } else {
            button.buttonStyle(.bordered).disabled(!row.editable || resolving || (selecting && !selectable))
        }
    }

    private func chipLabel(_ candidate: UpdateCandidate) -> String {
        switch candidate.step {
        case .patch: String(localized: "patch \(candidate.version)")
        case .minor: String(localized: "minor \(candidate.version)")
        case .major: String(localized: "major \(candidate.version)")
        }
    }

    private func automationLabel(_ automation: UpdateAutomation) -> String {
        switch automation {
        case .fluxImage: String(localized: "Flux image automation")
        case .argoImageUpdater: String(localized: "Argo CD Image Updater")
        case .semverRange: String(localized: "Follows a semver range")
        case .bot: String(localized: "Bot configured")
        }
    }
}
