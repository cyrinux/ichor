import SwiftUI
import IchorCore

/// The Kubernetes events of an object as list rows, newest first, like the end of `kubectl
/// describe`: of the `kind` named `name`, or with an empty `kind` of `name` and what it owns (a
/// Deployment's ReplicaSets and pods). Read once when shown.
struct KubeEventsRows: View {
    let namespace: String
    let kind: String
    let name: String

    // Explicit: the private @State makes the memberwise init private.
    init(namespace: String, kind: String, name: String) {
        self.namespace = namespace
        self.kind = kind
        self.name = name
    }

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<[KubeEvent]> = .loading
    @State private var all = false

    private let preview = 8

    var body: some View {
        Group {
            switch state {
            case .loading:
                ProgressView().frame(maxWidth: .infinity)
            case .failed(let message):
                ErrorOrNoticeText(message: message)
            case .loaded(let events, _, _):
                let now = Int64(Date().timeIntervalSince1970 * 1000)
                if events.isEmpty {
                    Text(verbatim: CheckupText.kubeEventsEmpty).font(.caption).foregroundStyle(.secondary)
                }
                ForEach(Array((all ? events : Array(events.prefix(preview))).enumerated()), id: \.offset) { _, event in
                    KubeEventRow(event: event, now: now, showObject: kind.isEmpty)
                }
                if events.count > preview {
                    Button {
                        all.toggle()
                    } label: {
                        Text(verbatim: all ? CheckupText.checkupShowLess : CheckupText.checkupShowAll("\(events.count)")).font(.callout)
                    }
                }
            }
        }
        .task(id: "\(namespace)/\(kind)/\(name)") { await load() }
    }

    private func load() async {
        guard let client = model.client else { return }
        state = await .from { try await client.kubeEvents(namespace: namespace, kind: kind, name: name) }
    }
}

/// One event: its reason (amber for a Warning), when it was last seen, how often, and its message.
private struct KubeEventRow: View {
    let event: KubeEvent
    let now: Int64
    let showObject: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 6) {
                InfoChip(text: event.reason, color: event.isWarning ? Color.statusWarn : nil)
                if event.count > 1 { Text(verbatim: "× \(event.count)").font(.caption).foregroundStyle(.secondary) }
                Spacer()
                Text(verbatim: CheckupText.kubeEventsAgo(checkupAge(event.last, now: now))).font(.caption).foregroundStyle(.secondary)
            }
            if showObject {
                Text(verbatim: "\(event.kind) \(event.name)").font(.caption2.monospaced()).foregroundStyle(.secondary).lineLimit(1).truncationMode(.middle)
            }
            Text(verbatim: event.message).font(.caption).lineLimit(6)
        }
        .padding(.vertical, 2)
    }
}

/// An object's events on a page of their own, pushed from a sheet.
struct KubeEventsPage: View {
    let namespace: String
    let kind: String
    let name: String

    var body: some View {
        List {
            Section { KubeEventsRows(namespace: namespace, kind: kind, name: name) }
        }
        .themedBackground()
        .navigationTitle(Text(verbatim: CheckupText.kubeEventsTitle))
        .navigationBarTitleDisplayMode(.inline)
    }
}
