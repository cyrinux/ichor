import SwiftUI
import IchorCore

/// Live `talosctl logs -f`: lines are buffered and published a few times a second, so a busy
/// log does not re-render the list for every line. Each batch is parsed (Go ParseLogLine) and
/// styled off the main actor.
@Observable
@MainActor
final class LogFollower {
    static let tailLines = 200
    static let maxLines = 5000
    static let flushMillis = 250

    private(set) var document = LogDocument()
    private(set) var error: String?
    private(set) var active = false
    private var pending: [String] = []
    private var nextID = 0

    /// Follows a Kubernetes container's log when `containerID` is set, else the service's (the
    /// kernel log when `service` is nil).
    func run(_ client: TalosClient, node: String, service: String?, containerID: String? = nil) async {
        let stream = containerID.map { client.followContainerLogs(node: node, containerID: $0, tailLines: Self.tailLines) }
            ?? client.followLogs(node: node, service: service, tailLines: Self.tailLines)
        await follow(stream)
    }

    /// Follows `stream` (a Talos or a Kubernetes log) from an empty document, until it ends or
    /// the task is cancelled.
    func follow(_ stream: AsyncStream<LogFollowItem>) async {
        document = LogDocument()
        pending = []
        error = nil
        active = true
        defer { active = false }
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
        await flush()
    }

    private func consume(_ stream: AsyncStream<LogFollowItem>) async {
        for await item in stream {
            switch item {
            case .line(let line): pending.append(line)
            case .done(let failure): if !Task.isCancelled { error = failure }
            }
        }
    }

    private func flush() async {
        guard !pending.isEmpty else { return }
        let batch = pending
        let firstID = nextID
        nextID += batch.count
        pending = []
        let (items, texts) = await Task.detached(priority: .userInitiated) {
            let items = batch.enumerated().map { LogItem(id: firstID + $0.offset, entry: TalosClient.parseLogLine($0.element)) }
            return (items, LogStyle.texts(items))
        }.value
        document = document.appending(items, texts: texts, cap: Self.maxLines)
    }
}

/// Display options shared by the snapshot and the followed list.
struct LogDisplay {
    var search = ""
    var level: LogLevelFilter = .all
    var raw = false
}

/// A Kubernetes container whose log is shown: its CRI id and the name for the title.
struct LogContainer: Hashable {
    let id: String
    let name: String
}

/// Last 500 lines of a service log, of a container's log, or the kernel log (dmesg) when
/// both are nil; "Follow" streams new lines instead.
struct LogsView: View {
    let node: String
    let hostname: String
    let service: String?
    let container: LogContainer?

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<LogDocument> = .loading
    @State private var display = LogDisplay()
    @State private var following = false
    @State private var follower = LogFollower()

    init(node: String, hostname: String, service: String?, container: LogContainer? = nil) {
        self.node = node
        self.hostname = hostname
        self.service = service
        self.container = container
    }

    var body: some View {
        Group {
            if following {
                FollowList(follower: follower, display: $display)
            } else {
                LoadStateView(state: state, retry: load) { document in
                    SnapshotList(document: document, display: $display)
                }
            }
        }
        .searchable(text: $display.search, prompt: "Filter")
        .navigationTitle(container?.name ?? service ?? String(localized: "Kernel log"))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItemGroup(placement: .primaryAction) {
                Toggle(isOn: $following) {
                    Label("Follow", systemImage: "dot.radiowaves.left.and.right")
                }
                .toggleStyle(.button)
                if !following {
                    Button { Task { await load() } } label: { Image(systemName: "arrow.clockwise") }
                        .accessibilityLabel(Text("Refresh"))
                }
                Menu {
                    Toggle(isOn: $display.raw) {
                        Label("Raw lines", systemImage: "text.alignleft")
                    }
                } label: {
                    Image(systemName: "ellipsis.circle").accessibilityLabel(Text("More actions"))
                }
            }
        }
        // Follows while toggled on and visible: the task is cancelled on toggle off or disappear.
        .task(id: following) {
            if following {
                guard let client = model.client else { return }
                await follower.run(client, node: node, service: service, containerID: container?.id)
            } else {
                await load()
            }
        }
    }

    private func load() async {
        guard let client = model.client else { return }
        state = .loading
        state = await .from {
            let tail: LogTail
            if let container {
                tail = try await client.containerLogs(node: node, containerID: container.id)
            } else {
                tail = try await client.logs(node: node, service: service)
            }
            return await Task.detached(priority: .userInitiated) { LogDocument(tail) }.value
        }
    }
}

/// Parsed rows (or raw lines) of `document` for the current search and level filter, with the
/// level counts of the searched lines.
private struct LogListContent {
    let rows: [LogRow]
    let rawItems: [LogItem]
    let counts: LogLevelCounts

    init(_ document: LogDocument, _ display: LogDisplay) {
        let searched = searchLogs(document.items, display.search)
        if display.raw {
            rows = []
            rawItems = searched
            counts = LogLevelCounts(all: searched.count, warnings: 0, errors: 0)
        } else {
            rows = logRows(collapseLogs(searched.filter { display.level.matches($0.entry.logLevel) }))
            rawItems = []
            counts = logLevelCounts(searched)
        }
    }

    var lastID: Int? { rawItems.last?.id ?? rows.last?.id }
    var isEmpty: Bool { rows.isEmpty && rawItems.isEmpty }
}

/// Rows of a log list: raw lines, or parsed rows with tap-to-expand.
private struct LogListRows: View {
    let content: LogListContent
    let texts: [Int: AttributedString]
    @Binding var expanded: Set<Int>

    var body: some View {
        ForEach(content.rawItems) { LogRawLine(text: $0.entry.raw) }
        ForEach(content.rows) { LogRowView(row: $0, texts: texts, expanded: $expanded) }
    }
}

/// Level picker above the list, except in raw mode.
private struct LevelBar: ViewModifier {
    @Binding var display: LogDisplay
    let counts: LogLevelCounts

    func body(content: Content) -> some View {
        content.safeAreaInset(edge: .top, spacing: 0) {
            if !display.raw {
                LogLevelPicker(level: $display.level, counts: counts)
            }
        }
    }
}

/// A loaded log tail, scrolled to the newest line.
private struct SnapshotList: View {
    let document: LogDocument
    @Binding var display: LogDisplay

    @State private var expanded: Set<Int> = []

    var body: some View {
        let content = LogListContent(document, display)
        ScrollViewReader { proxy in
            List {
                if document.truncated && display.search.isEmpty {
                    Text("… older lines omitted").font(.caption2).foregroundStyle(.secondary)
                }
                LogListRows(content: content, texts: document.texts, expanded: $expanded)
            }
            .listStyle(.plain)
            .scalableMonoText(.logs)
            .themedBackground()
            .modifier(LevelBar(display: $display, counts: content.counts))
            .overlay {
                if content.isEmpty && !document.items.isEmpty {
                    ContentUnavailableView("No matching lines", systemImage: "line.3.horizontal.decrease.circle")
                }
            }
            .onAppear {
                if let last = content.lastID { proxy.scrollTo(last, anchor: .bottom) }
            }
        }
    }
}

/// Followed lines; keeps the newest line in view unless the user scrolled up to read.
struct FollowList: View {
    let follower: LogFollower
    @Binding var display: LogDisplay

    // Explicit: the private @State makes the memberwise init private.
    init(follower: LogFollower, display: Binding<LogDisplay>) {
        self.follower = follower
        _display = display
    }

    @State private var pinned = true
    @State private var expanded: Set<Int> = []

    private static let bottomID = "bottom"

    var body: some View {
        let document = follower.document
        let content = LogListContent(document, display)
        ScrollViewReader { proxy in
            List {
                if let error = follower.error { ErrorOrNoticeText(message: error) }
                LogListRows(content: content, texts: document.texts, expanded: $expanded)
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
            .scalableMonoText(.logs)
            .themedBackground()
            .modifier(LevelBar(display: $display, counts: content.counts))
            .modifier(PinnedToBottom(pinned: $pinned))
            // The newest item, not the newest row: a repeated line only grows its row's count.
            .onChange(of: document.items.last?.id) {
                if pinned { proxy.scrollTo(Self.bottomID, anchor: .bottom) }
            }
            .overlay(alignment: .bottomTrailing) {
                if !pinned && !content.isEmpty {
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
                let atBottom = new.distanceToBottom < 60
                // Re-rendering re-filters up to 5000 lines: only on an actual change.
                if pinned != atBottom { pinned = atBottom }
            }
        } else {
            content
        }
    }
}
