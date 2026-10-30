import SwiftUI
import UniformTypeIdentifiers
import IchorCore

/// The logs of a workload's pods merged by time, like `kubectl logs -l`, a chip per pod, with a
/// filter, a time range, copy and share. Each change reads the logs again.
struct WorkloadLogsSheet: View {
    let workload: KubeWorkload

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<WorkloadLogs> = .loading
    @State private var filter = LogFilter()
    @State private var tail = workloadLogTails.last ?? 2000
    @State private var refreshes = 0
    /// The latest load: an older one (the Go call ignores cancellation) never lands after it.
    @State private var generation = 0
    /// The log written out for the share sheet: it can be larger than a share carries as text.
    @State private var shareFile: URL?
    @State private var message: String?

    var body: some View {
        VStack(spacing: 0) {
            VStack(alignment: .leading, spacing: 8) {
                LogFilterFields(filter: $filter)
                Picker("Lines per pod", selection: $tail) {
                    ForEach(workloadLogTails, id: \.self) { Text("\($0) lines per pod").tag($0) }
                }
            }
            .padding(.horizontal)
            .padding(.vertical, 8)
            LoadStateView(state: state, retry: { refreshes += 1 }) { logs in
                MergedLog(logs: logs)
            }
        }
        .themedBackground()
        .navigationTitle(Text(verbatim: workload.name))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItemGroup(placement: .primaryAction) {
                if case .loaded(let logs, _, _) = state, !logs.lines.isEmpty {
                    Menu {
                        Button {
                            copy(logs.asText)
                        } label: {
                            Label("Copy", systemImage: "doc.on.doc")
                        }
                        if let shareFile {
                            ShareLink(item: shareFile) { Label("Share", systemImage: "square.and.arrow.up") }
                        }
                    } label: {
                        Image(systemName: "ellipsis.circle").accessibilityLabel(Text("More actions"))
                    }
                }
                Button {
                    refreshes += 1
                } label: {
                    Image(systemName: "arrow.clockwise")
                }
                .accessibilityLabel(Text("Refresh"))
            }
        }
        // The screenshot mode's generation too: the log is read again under the new names.
        .task(id: "\(filter)/\(tail)/\(refreshes)/\(model.dataGeneration)") { await load() }
        .onDisappear { removeShareFile() }
        .messageAlert($message)
    }

    private func load() async {
        guard let client = model.client else { return }
        generation += 1
        let mine = generation
        let current = { !Task.isCancelled && mine == generation }
        state = .loading
        do {
            let logs = try await client.workloadLogs(workload, filter: filter, tailLines: tail)
            guard current() else { return }
            writeShareFile(logs)
            state = .loaded(logs, at: Date())
        } catch {
            guard current() else { return }
            state = .failed(error.localizedDescription)
        }
    }

    private func writeShareFile(_ logs: WorkloadLogs) {
        removeShareFile()
        let url = FileManager.default.temporaryDirectory.appendingPathComponent("\(workload.name).log")
        // Without the file the menu still offers Copy.
        if (try? Data(logs.asText.utf8).write(to: url, options: [.atomic, .completeFileProtection])) != nil {
            shareFile = url
        }
    }

    private func removeShareFile() {
        if let shareFile { try? FileManager.default.removeItem(at: shareFile) }
        shareFile = nil
    }

    /// Device-only pasteboard: a log can hold secrets.
    private func copy(_ text: String) {
        UIPasteboard.general.setItems([[UTType.utf8PlainText.identifier: text]], options: [.localOnly: true])
        message = String(localized: "Copied.")
    }
}

/// The merged lines, scrolled to the newest, and how many matched.
private struct MergedLog: View {
    let logs: WorkloadLogs

    var body: some View {
        if logs.lines.isEmpty {
            ContentUnavailableView("No log yet", systemImage: "doc.text")
                .safeAreaInset(edge: .bottom) { footer }
        } else {
            // A List, as PodLogsSheet's: a lazy stack bounces over wrapped lines.
            ScrollViewReader { proxy in
                List(logs.lines.indices, id: \.self) { index in
                    MergedLogRow(line: logs.lines[index], slot: logs.colorSlot(of: logs.lines[index].pod))
                        .listRowInsets(EdgeInsets(top: 1, leading: 16, bottom: 1, trailing: 16))
                        .listRowSeparator(.hidden)
                        .listRowBackground(Color.clear)
                }
                .listStyle(.plain)
                .scalableMonoText(.logs)
                .scrollContentBackground(.hidden)
                .onAppear { proxy.scrollTo(logs.lines.count - 1, anchor: .bottom) }
            }
            .safeAreaInset(edge: .bottom) { footer }
        }
    }

    /// "Matched 42 of 3,120 · truncated".
    private var footer: some View {
        HStack(spacing: 4) {
            Text("Matched \(logs.matched) of \(logs.scanned)")
            if logs.truncated {
                Text(verbatim: "·")
                Text("truncated")
            }
        }
        .font(.caption)
        .foregroundStyle(.secondary)
        .monospacedDigit()
        .frame(maxWidth: .infinity)
        .padding(.vertical, 6)
        .background(.bar)
    }
}

private struct MergedLogRow: View {
    let line: WorkloadLogLine
    let slot: Int

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            Text(verbatim: line.podSuffix)
                .font(.caption.monospaced())
                .lineLimit(1)
                .padding(.horizontal, 4)
                .background(Self.colors[slot].opacity(0.35), in: RoundedRectangle(cornerRadius: 4, style: .continuous))
                .accessibilityLabel(Text("Pod \(line.pod)"))
            if line.time > 0 {
                Text(Date(timeIntervalSince1970: Double(line.time) / 1000), format: .dateTime.hour().minute().second())
                    .font(.caption.monospacedDigit())
                    .foregroundStyle(.secondary)
            }
            Text(verbatim: line.text)
                .monoFont()
                .foregroundStyle(textColor)
                .frame(maxWidth: .infinity, alignment: .leading)
                .textSelection(.enabled)
        }
    }

    private var textColor: Color {
        switch line.level {
        case "error": .statusBad
        case "warn", "warning": .statusWarn
        default: .primary
        }
    }

    /// The pods' colours, tinting a chip under the theme's text: one per `WorkloadLogPod.color`.
    private static let colors: [Color] = [
        .blue, .orange, .green, .purple, .pink, .teal, .yellow, .gray,
    ]
}
