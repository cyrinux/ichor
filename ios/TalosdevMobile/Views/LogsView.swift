import SwiftUI
import TalosdevMobileCore

/// Last 500 lines of a service log, or the kernel log (dmesg) when `service` is nil.
struct LogsView: View {
    let node: String
    let hostname: String
    let service: String?

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<LogTail> = .loading
    @State private var filter = ""

    init(node: String, hostname: String, service: String?) {
        self.node = node
        self.hostname = hostname
        self.service = service
    }

    var body: some View {
        LoadStateView(state: state, retry: load) { tail in
            let lines = filter.isEmpty ? tail.lines : tail.lines.filter { $0.localizedCaseInsensitiveContains(filter) }
            ScrollViewReader { proxy in
                List {
                    if tail.truncated && filter.isEmpty {
                        Text("… older lines omitted").font(.caption2).foregroundStyle(.secondary)
                    }
                    ForEach(Array(lines.enumerated()), id: \.offset) { index, line in
                        Text(line)
                            .font(.system(size: 11, design: .monospaced))
                            .textSelection(.enabled)
                            .id(index)
                    }
                }
                .listStyle(.plain)
                .themedBackground()
                .onAppear { proxy.scrollTo(lines.count - 1, anchor: .bottom) }
            }
        }
        .searchable(text: $filter, prompt: "Filter")
        .navigationTitle(service ?? "Kernel log")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button { Task { await load() } } label: { Image(systemName: "arrow.clockwise") }
            }
        }
        .task { await load() }
    }

    private func load() async {
        guard let client = model.client else { return }
        state = .loading
        state = await .from { try await client.logs(node: node, service: service) }
    }
}
