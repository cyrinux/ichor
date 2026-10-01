import SwiftUI
import TalosdevMobileCore

/// Live `talosctl logs -f`: lines are buffered and published a few times a second, so a busy
/// log does not re-render the list for every line.
@Observable
@MainActor
final class LogFollower {
    static let tailLines = 200
    static let maxLines = 5000
    static let flushMillis = 250

    private(set) var lines: [LogLine] = []
    private(set) var error: String?
    private(set) var active = false
    private var pending: [String] = []
    private var nextID = 0

    func run(_ client: TalosClient, node: String, service: String?) async {
        lines = []
        pending = []
        error = nil
        active = true
        defer { active = false }
        let stream = client.followLogs(node: node, service: service, tailLines: Self.tailLines)
        let interval = Self.flushMillis
        await withTaskGroup(of: Void.self) { group in
            group.addTask { await self.consume(stream) }
            group.addTask {
                while !Task.isCancelled {
                    try? await Task.sleep(for: .milliseconds(interval))
                    await self.flush()
                }
            }
            // The stream ended (or the view went away): stop the flush loop too.
            _ = await group.next()
            group.cancelAll()
        }
        flush()
    }

    private func consume(_ stream: AsyncStream<LogFollowItem>) async {
        for await item in stream {
            switch item {
            case .line(let line): pending.append(line)
            case .done(let failure): if !Task.isCancelled { error = failure }
            }
        }
    }

    private func flush() {
        guard !pending.isEmpty else { return }
        let new = pending.enumerated().map { LogLine(id: nextID + $0.offset, text: $0.element) }
        nextID += pending.count
        pending = []
        lines = appendCapped(lines, new, cap: Self.maxLines)
    }
}

/// Last 500 lines of a service log, or the kernel log (dmesg) when `service` is nil; "Follow"
/// streams new lines instead.
struct LogsView: View {
    let node: String
    let hostname: String
    let service: String?

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<LogTail> = .loading
    @State private var filter = ""
    @State private var following = false
    @State private var follower = LogFollower()

    init(node: String, hostname: String, service: String?) {
        self.node = node
        self.hostname = hostname
        self.service = service
    }

    var body: some View {
        Group {
            if following {
                FollowList(follower: follower, filter: filter)
            } else {
                LoadStateView(state: state, retry: load) { tail in
                    snapshot(tail)
                }
            }
        }
        .searchable(text: $filter, prompt: "Filter")
        .navigationTitle(service ?? String(localized: "Kernel log"))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItemGroup(placement: .primaryAction) {
                Toggle(isOn: $following) {
                    Label("Follow", systemImage: "dot.radiowaves.left.and.right")
                }
                .toggleStyle(.button)
                if !following {
                    Button { Task { await load() } } label: { Image(systemName: "arrow.clockwise") }
                }
            }
        }
        // Follows while toggled on and visible: the task is cancelled on toggle off or disappear.
        .task(id: following) {
            if following {
                guard let client = model.client else { return }
                await follower.run(client, node: node, service: service)
            } else {
                await load()
            }
        }
    }

    private func snapshot(_ tail: LogTail) -> some View {
        let lines = filter.isEmpty ? tail.lines : tail.lines.filter { $0.localizedCaseInsensitiveContains(filter) }
        return ScrollViewReader { proxy in
            List {
                if tail.truncated && filter.isEmpty {
                    Text("… older lines omitted").font(.caption2).foregroundStyle(.secondary)
                }
                ForEach(Array(lines.enumerated()), id: \.offset) { index, line in
                    LogLineText(text: line).id(index)
                }
            }
            .listStyle(.plain)
            .themedBackground()
            .onAppear { proxy.scrollTo(lines.count - 1, anchor: .bottom) }
        }
    }

    private func load() async {
        guard let client = model.client else { return }
        state = .loading
        state = await .from { try await client.logs(node: node, service: service) }
    }
}

/// Followed lines; keeps the newest line in view unless the user scrolled up to read.
private struct FollowList: View {
    let follower: LogFollower
    let filter: String

    @State private var pinned = true

    private static let bottomID = "bottom"

    var body: some View {
        let lines = filter.isEmpty ? follower.lines : follower.lines.filter { $0.text.localizedCaseInsensitiveContains(filter) }
        ScrollViewReader { proxy in
            List {
                if let error = follower.error {
                    Text(error).font(.footnote).foregroundStyle(.red)
                }
                ForEach(lines) { LogLineText(text: $0.text) }
                HStack(spacing: 8) {
                    if follower.active {
                        ProgressView().controlSize(.small)
                        Text("Following…").font(.caption).foregroundStyle(.secondary)
                    }
                }
                .id(Self.bottomID)
                .listRowSeparator(.hidden)
                .onAppear { pinned = true }
            }
            .listStyle(.plain)
            .themedBackground()
            .modifier(PinnedToBottom(pinned: $pinned))
            .onChange(of: lines.last?.id) {
                if pinned { proxy.scrollTo(Self.bottomID, anchor: .bottom) }
            }
            .overlay(alignment: .bottomTrailing) {
                if !pinned && !lines.isEmpty {
                    Button {
                        pinned = true
                        proxy.scrollTo(Self.bottomID, anchor: .bottom)
                    } label: {
                        Image(systemName: "arrow.down.circle.fill").font(.title)
                    }
                    .padding()
                    .accessibilityLabel(Text("Scroll to the newest line"))
                }
            }
        }
    }
}

/// iOS 18+: unpins when the user scrolls away from the bottom, re-pins when back at it (content
/// growth alone does not unpin). On iOS 17 the list always follows the newest line.
private struct PinnedToBottom: ViewModifier {
    @Binding var pinned: Bool

    private struct Metrics: Equatable {
        var offset: CGFloat
        /// Negative or zero when the last row is fully visible.
        var distanceToBottom: CGFloat
    }

    func body(content: Content) -> some View {
        if #available(iOS 18.0, *) {
            content.onScrollGeometryChange(for: Metrics.self) { geometry in
                Metrics(offset: geometry.contentOffset.y,
                        distanceToBottom: geometry.contentSize.height - geometry.visibleRect.maxY)
            } action: { old, new in
                // Only a scroll (offset change) decides; appended lines just grow the content.
                guard old.offset != new.offset else { return }
                pinned = new.distanceToBottom < 60
            }
        } else {
            content
        }
    }
}

private struct LogLineText: View {
    let text: String

    var body: some View {
        Text(verbatim: text)
            .font(.system(size: 11, design: .monospaced))
            .textSelection(.enabled)
    }
}
