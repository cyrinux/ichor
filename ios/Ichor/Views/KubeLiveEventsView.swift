import SwiftUI
import IchorCore

/// The cluster's Kubernetes events kept live, newest first, for the namespace scope of the
/// Kubernetes screens (every namespace, or one): Warnings only and the scope restart the
/// stream, the search narrows the rows on screen. Pause holds new rows back ("N new") until
/// resumed. A row opens its object's summary when Go knows its resource. Streams only while
/// shown and the app active; share sends the rows on screen as text.
struct KubeLiveEventsView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.scenePhase) private var scenePhase
    @State private var scope = KubeBrowserScope()
    @State private var feed = KubeEventsFeed()
    /// nil until the stream says how it is doing: connecting.
    @State private var status: LiveKubeEventsStatus?
    @State private var error: String?
    @State private var warningsOnly = false
    @State private var query = ""
    /// Bumped by Retry: the stream starts again.
    @State private var runID = 0

    /// What decides the stream: the scope, the cluster, API address and screenshot mode, the
    /// filter, the app being active and Retry.
    private struct Trigger: Hashable {
        let scope: KubeScope
        let ready: Bool
        let source: String
        let warningsOnly: Bool
        let active: Bool
        let run: Int
    }

    var body: some View {
        let control = scope.control(model: model)
        let shown = feed.rows.filter { $0.matches(query) }
        VStack(spacing: 0) {
            KubeScopeBar(control: control, loaded: loadedNamespaces)
                .background(.bar)
            Divider()
            if !control.ready {
                ContentUnavailableView {
                    Label("Namespace", systemImage: "square.dashed")
                } description: {
                    Text("Type the namespace to list above.")
                }
                .frame(maxHeight: .infinity)
            } else {
                list(shown, namespace: control.scope.namespace)
            }
        }
        .searchable(text: $query, prompt: Text("Search reason, object or message"))
        .autocorrectionDisabled()
        .textInputAutocapitalization(.never)
        .navigationTitle(Text("Kubernetes events"))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItemGroup(placement: .primaryAction) {
                Button {
                    feed = feed.paused ? feed.resuming() : feed.pausing()
                } label: {
                    Image(systemName: feed.paused ? "play.fill" : "pause.fill")
                }
                .accessibilityLabel(feed.paused ? Text("Resume") : Text("Pause"))
                Menu {
                    Toggle(isOn: $warningsOnly) {
                        Label("Warnings only", systemImage: "exclamationmark.triangle")
                    }
                    ShareLink(item: shareText(shown, namespace: control.scope.namespace)) {
                        Label("Share", systemImage: "square.and.arrow.up")
                    }
                    .disabled(shown.isEmpty)
                } label: {
                    Image(systemName: warningsOnly ? "line.3.horizontal.decrease.circle.fill" : "ellipsis.circle")
                }
                .accessibilityLabel(Text("More"))
                .accessibilityValue(warningsOnly ? Text("Warnings only") : Text(verbatim: ""))
            }
        }
        .task(id: kubeNamespacesKey(model)) { await scope.loadNamespaces(model: model) }
        .task(id: Trigger(scope: control.scope, ready: control.ready, source: kubeNamespacesKey(model),
                          warningsOnly: warningsOnly, active: scenePhase == .active, run: runID)) {
            if control.ready { await stream(namespace: control.scope.namespace) }
        }
    }

    private func list(_ shown: [LiveKubeEvent], namespace: String?) -> some View {
        TimelineView(.periodic(from: .now, by: 30)) { context in
            let now = Int64(context.date.timeIntervalSince1970 * 1000)
            List {
                Section {
                    KubeEventsStatusLine(status: status)
                    if feed.paused {
                        Button { feed = feed.resuming() } label: {
                            HStack {
                                Label("Paused", systemImage: "pause.circle")
                                Spacer()
                                if feed.pendingCount > 0 {
                                    Text("\(feed.pendingCount) new").monospacedDigit()
                                }
                                Text("Resume").foregroundStyle(.tint)
                            }
                        }
                    }
                }
                if let error {
                    Section {
                        ErrorOrNoticeText(message: shownError(error, namespace: namespace))
                        Button("Retry") { runID += 1 }
                    }
                }
                Section {
                    ForEach(shown) { event in
                        if let resource = event.regarding.apiResource {
                            NavigationLink {
                                KubeObjectView(resource: resource, namespace: event.regarding.objectNamespace, name: event.regarding.name)
                            } label: {
                                LiveKubeEventRow(event: event, now: now)
                            }
                        } else {
                            LiveKubeEventRow(event: event, now: now)
                        }
                    }
                }
            }
            .overlay {
                if !feed.loaded && error == nil {
                    ProgressView()
                }
            }
            .emptyOverlay(feed.loaded && shown.isEmpty && error == nil, query: query) {
                ContentUnavailableView("No events", systemImage: "list.bullet.rectangle",
                                       description: Text("New events appear here as they happen."))
            }
            .themedBackground()
        }
    }

    private var loadedNamespaces: [String] {
        Array(Set(feed.rows.map(\.regarding.namespace).filter { !$0.isEmpty })).sorted()
    }

    /// A namespace the credentials may not read told as such.
    private func shownError(_ message: String, namespace: String?) -> String {
        guard let namespace, isKubeForbidden(message) else { return message }
        return String(localized: "No access to the namespace \(namespace): pick or type another one.")
    }

    private func shareText(_ rows: [LiveKubeEvent], namespace: String?) -> String {
        let title = String(localized: "Kubernetes events")
        let scope = namespace ?? String(localized: "All namespaces")
        return kubeEventsShareText(rows, header: "\(title) · \(scope)")
    }

    /// Follows the events until the task is cancelled (the screen leaves, the app goes to the
    /// background, the scope or filter changes); a pause lasts across a restart.
    private func stream(namespace: String?) async {
        guard let client = model.client, scenePhase == .active else { return }
        feed = KubeEventsFeed(paused: feed.paused)
        status = nil
        error = nil
        for await item in client.liveKubeEvents(namespace: namespace, warningsOnly: warningsOnly) {
            switch item {
            case .batch(let batch):
                feed = feed.receiving(batch)
            case .status(let next):
                status = next
            case .done(let failure):
                if !Task.isCancelled { error = failure }
            }
        }
    }
}

/// How the stream is doing: connecting, live, reconnecting or relisting (with why), polling,
/// and a note when rows were let go or the first list was cut.
private struct KubeEventsStatusLine: View {
    let status: LiveKubeEventsStatus?

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Circle()
                .fill(color)
                .frame(width: 8, height: 8)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                title.font(.subheadline)
                if let reason = status?.reason.nonEmpty {
                    Text(verbatim: reason).font(.caption).foregroundStyle(.secondary).lineLimit(3)
                }
                if let note = status?.note.nonEmpty {
                    Text(verbatim: note).font(.caption).foregroundStyle(.secondary)
                }
            }
        }
        .accessibilityElement(children: .combine)
    }

    private var title: Text {
        guard let status else { return Text("Connecting…") }
        switch status.liveState {
        case .live: return Text("Live")
        case .reconnecting: return Text("Reconnecting")
        case .relisting: return Text("Relisting")
        case .polling: return Text("Polling")
        }
    }

    private var color: Color {
        guard let status else { return .secondary }
        switch status.liveState {
        case .live: return .statusOK
        case .reconnecting: return .statusWarn
        case .relisting, .polling: return .accentColor
        }
    }
}

/// One coalesced event: its type (amber for a Warning), reason, how often, when it was last
/// seen, the object it is about and its latest note.
private struct LiveKubeEventRow: View {
    let event: LiveKubeEvent
    let now: Int64

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: event.isWarning ? "exclamationmark.triangle.fill" : "info.circle")
                .foregroundStyle(event.isWarning ? Color.statusWarn : Color.secondary)
                .frame(width: 20)
                .accessibilityLabel(event.isWarning ? Text("Warning") : Text("Normal"))
            VStack(alignment: .leading, spacing: 3) {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Text(verbatim: event.reason)
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(event.isWarning ? Color.statusWarn : Color.primary)
                        .lineLimit(1)
                    if event.count > 1 {
                        Text(verbatim: "×\(event.count)")
                            .font(.caption.weight(.semibold))
                            .monospacedDigit()
                            .padding(.horizontal, 6)
                            .padding(.vertical, 1)
                            .background(Color.secondary.opacity(0.15), in: Capsule())
                            .accessibilityLabel(Text("Repeated \(event.count) times"))
                    }
                    Spacer(minLength: 4)
                    if event.lastSeen > 0 {
                        Text(verbatim: CheckupText.kubeEventsAgo(checkupAge(event.lastSeen, now: now)))
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .monospacedDigit()
                    }
                }
                Text(verbatim: event.regarding.label)
                    .font(.caption.monospaced())
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .truncationMode(.middle)
                if !event.note.isEmpty {
                    Text(verbatim: event.note).font(.caption).lineLimit(2)
                }
            }
        }
        .padding(.vertical, 2)
        .accessibilityElement(children: .combine)
    }
}
