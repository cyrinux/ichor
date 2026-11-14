import SwiftUI
import IchorCore

/// "Top messages" of a loaded log: its repeated messages with their count and share, the parts
/// that change from line to line (numbers, ids, addresses, durations) dimmed. A tap dismisses the
/// sheet and filters the log on that message (`onPick` gets its newest line as written). `load`
/// groups the lines once, off the main actor; `lineCount` under `logTemplatesMinLines` says why
/// there is nothing to group. As Android's LogTemplatesSheet.
struct LogTemplatesSheet: View {
    let lineCount: Int
    let load: @Sendable () async throws -> LogTemplates
    let onPick: (String) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var state: LoadState<LogTemplates> = .loading

    var body: some View {
        NavigationStack {
            Group {
                if lineCount < logTemplatesMinLines {
                    ContentUnavailableView {
                        Label("Top messages", systemImage: "list.number")
                    } description: {
                        Text("Top messages groups a log of at least \(logTemplatesMinLines) lines.")
                    }
                } else {
                    LoadStateView(state: state, retry: reload) { templates in
                        List {
                            Section {
                                ForEach(templates.groups) { group in
                                    Button {
                                        onPick(group.sample)
                                        dismiss()
                                    } label: {
                                        TemplateRow(group: group)
                                    }
                                    .buttonStyle(.plain)
                                }
                            } footer: {
                                Text(
                                    "Numbers, ids, addresses and durations are masked as <*>. Tap a message to filter the log on it."
                                )
                            }
                        }
                    }
                }
            }
            .navigationTitle("Top messages")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Done") { dismiss() } }
            }
            .task { if lineCount >= logTemplatesMinLines { await reload() } }
        }
    }

    private func reload() async {
        state = await LoadState.from { try await load() }
    }
}

/// One message: its count and share, a share bar, the template with its <*> dimmed, and the pods.
private struct TemplateRow: View {
    let group: LogTemplateGroup

    private var tint: Color {
        switch group.level.lowercased() {
        case "error": .statusBad
        case "warn", "warning": .statusWarn
        default: .secondary
        }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text("\(group.count) × · \(group.shareText)").font(.caption.weight(.semibold)).foregroundStyle(tint)
                if group.pods > 0 {
                    Text("in \(group.pods) pods").font(.caption).foregroundStyle(.secondary)
                }
            }
            ProgressView(value: min(max(group.share, 0), 1)).tint(tint == .secondary ? .accentColor : tint)
            group.parts.reduce(Text(verbatim: "")) { text, part in
                text + Text(verbatim: part.text).foregroundColor(part.hole ? .secondary : .primary)
            }
            .font(.caption.monospaced())
        }
        .accessibilityElement(children: .combine)
    }
}
