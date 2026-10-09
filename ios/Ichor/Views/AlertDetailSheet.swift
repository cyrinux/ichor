import SwiftUI
import IchorCore

/// One Alertmanager alert: what it says (summary, description), since when, its state and
/// receivers, its labels and other annotations; its runbook and the query behind it; the pod,
/// namespace or node it is about; and a silence for it.
struct AlertDetailSheet: View {
    let alert: AMAlert
    /// The Talos node the alert is about, when its labels name one of the cluster's.
    let node: NodeOverview?
    /// The Kubernetes screen can be opened (the role may use the Kubernetes API).
    let canOpenKube: Bool
    /// Creates the silence; nil when done, else why not.
    let silence: (_ matchers: [AMMatcher], _ minutes: Int, _ comment: String) async -> String?
    /// Opens a screen over the alerts once the sheet is gone.
    let open: (Route) -> Void
    /// The silence was created: the sheet closes.
    let silenced: () -> Void
    /// Silence 1 h on the alert's notification: the silence form opens at once, set to 1 h with
    /// this comment (both can be changed, nothing is sent before "Silence").
    let silenceComment: String?

    @Environment(\.dismiss) private var dismiss
    @State private var silencing: Bool

    init(alert: AMAlert, node: NodeOverview?, canOpenKube: Bool,
         silence: @escaping (_ matchers: [AMMatcher], _ minutes: Int, _ comment: String) async -> String?,
         open: @escaping (Route) -> Void, silenced: @escaping () -> Void, silenceComment: String? = nil) {
        self.alert = alert
        self.node = node
        self.canOpenKube = canOpenKube
        self.silence = silence
        self.open = open
        self.silenced = silenced
        self.silenceComment = silenceComment
        _silencing = State(initialValue: silenceComment != nil)
    }

    var body: some View {
        NavigationStack {
            List {
                summarySection
                linksSection
                Section {
                    NavigationLink {
                        SilenceForm(alert: alert, silence: silence, done: silenced)
                    } label: {
                        Label("Silence…", systemImage: "bell.slash")
                    }
                }
                Section("Labels") {
                    ForEach(alert.labels.sorted { $0.key < $1.key }, id: \.key) { label in
                        LabeledContent {
                            Text(verbatim: label.value).textSelection(.enabled)
                        } label: {
                            Text(verbatim: label.key).font(.callout.monospaced())
                        }
                    }
                }
                if !alert.otherAnnotations.isEmpty {
                    Section("Annotations") {
                        ForEach(alert.otherAnnotations, id: \.key) { annotation in
                            VStack(alignment: .leading, spacing: 2) {
                                Text(verbatim: annotation.key).font(.caption.monospaced()).foregroundStyle(.secondary)
                                Text(verbatim: annotation.value).textSelection(.enabled)
                            }
                        }
                    }
                }
            }
            .navigationTitle(Text(verbatim: alert.alertname))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Close") { dismiss() } }
            }
            .navigationDestination(isPresented: $silencing) {
                SilenceForm(alert: alert, minutes: 60, comment: silenceComment ?? "", silence: silence, done: silenced)
            }
        }
    }

    private var summarySection: some View {
        Section {
            Label {
                Text(alert.severity.label).foregroundStyle(alert.severity.color)
            } icon: {
                Image(systemName: alert.severity.symbol).foregroundStyle(alert.severity.color)
            }
            if !alert.summary.isEmpty {
                Text(verbatim: alert.summary).font(.headline).textSelection(.enabled)
            }
            if !alert.description.isEmpty {
                Text(verbatim: alert.description).font(.callout).textSelection(.enabled)
            }
            LabeledContent("State", value: alert.stateLabel)
            if alert.startsAt > 0 {
                LabeledContent("Started", value: relativeTime(alert.startsAt))
            }
            if !alert.receivers.isEmpty {
                LabeledContent("Receivers", value: alert.receivers.joined(separator: ", "))
            }
            if alert.silenced {
                LabeledContent("Silenced by") {
                    Text(verbatim: alert.silencedBy.joined(separator: "\n")).font(.caption.monospaced()).textSelection(.enabled)
                }
            }
        }
    }

    /// The runbook, the query that raised the alert, and what the alert is about in the app.
    @ViewBuilder private var linksSection: some View {
        let runbook = webURL(alert.runbookURL)
        let generator = webURL(alert.generatorURL)
        let namespace = alert.labels["namespace"] ?? ""
        let pod = alert.labels["pod"] ?? ""
        if runbook != nil || generator != nil || node != nil || (canOpenKube && !namespace.isEmpty) {
            Section {
                if let runbook {
                    Link(destination: runbook) { Label("Runbook", systemImage: "book") }
                }
                if let generator {
                    Link(destination: generator) { Label("Open the alert's query", systemImage: "chart.xyaxis.line") }
                }
                if canOpenKube && !namespace.isEmpty && !pod.isEmpty {
                    let podID = "\(namespace)/\(pod)"
                    Button {
                        open(.kubernetes(KubeFocus(tab: .pods, id: podID, namespace: namespace, name: pod)))
                    } label: {
                        Label(String(localized: "Pod \(podID)"), systemImage: "cube")
                    }
                }
                if canOpenKube && !namespace.isEmpty {
                    Button {
                        open(.kubernetes(KubeFocus(tab: .workloads, namespace: namespace)))
                    } label: {
                        Label(String(localized: "Namespace \(namespace)"), systemImage: "square.stack.3d.up")
                    }
                }
                if let node {
                    Button {
                        open(.node(NodeRef(address: node.node, hostname: node.hostname, role: node.role)))
                    } label: {
                        Label(String(localized: "Node \(node.hostname)"), systemImage: "server.rack")
                    }
                }
            }
        }
    }

    /// An http(s) link; nil for anything else (no other scheme is opened from an alert).
    private func webURL(_ text: String) -> URL? {
        guard let url = URL(string: text), let scheme = url.scheme?.lowercased(), scheme == "https" || scheme == "http" else { return nil }
        return url
    }
}

/// Silences an alert: the matchers that mute exactly it (from Go, as removable chips to widen the
/// silence), for how long, and why (required). The outcome stays on the form when it fails.
private struct SilenceForm: View {
    let alert: AMAlert
    let silence: (_ matchers: [AMMatcher], _ minutes: Int, _ comment: String) async -> String?
    let done: () -> Void

    /// A preset duration in minutes, or 0 for the custom one.
    @State private var minutes: Int
    @State private var custom = ""
    @State private var matchers: [AMMatcher] = []
    @State private var comment: String

    init(alert: AMAlert, minutes: Int = amSilenceDurations[0], comment: String = "",
         silence: @escaping (_ matchers: [AMMatcher], _ minutes: Int, _ comment: String) async -> String?,
         done: @escaping () -> Void) {
        self.alert = alert
        self.silence = silence
        self.done = done
        _minutes = State(initialValue: minutes)
        _comment = State(initialValue: comment)
    }
    @State private var busy = false
    @State private var failure: String?

    private var duration: Int? { minutes > 0 ? minutes : amParseDuration(custom) }

    private var ready: Bool {
        !busy && !matchers.isEmpty && duration != nil && !comment.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    var body: some View {
        Form {
            Section {
                ChipFlow(spacing: 8) {
                    ForEach(matchers) { matcher in
                        Button {
                            withAnimation { matchers.removeAll { $0 == matcher } }
                        } label: {
                            HStack(spacing: 4) {
                                Text(verbatim: matcher.text).font(.caption.monospaced()).lineLimit(1)
                                Image(systemName: "xmark.circle.fill").foregroundStyle(.secondary)
                            }
                            .padding(.horizontal, 10)
                            .padding(.vertical, 6)
                            .background(Color(.tertiarySystemFill), in: Capsule())
                        }
                        .buttonStyle(.plain)
                        .accessibilityLabel(Text("Remove \(matcher.text)"))
                    }
                }
                .padding(.vertical, 4)
                if matchers.isEmpty {
                    Text("A silence needs at least one matcher.").font(.footnote).foregroundStyle(.red)
                }
            } header: {
                Text("Matchers")
            } footer: {
                Text("As set, the silence mutes only this alert. Remove a matcher to mute more alerts.")
            }
            Section("Duration") {
                Picker("Duration", selection: $minutes) {
                    Text(verbatim: "1h").tag(60)
                    Text(verbatim: "4h").tag(240)
                    Text(verbatim: "1d").tag(1440)
                    Text(verbatim: "1w").tag(10_080)
                    Text("Custom").tag(0)
                }
                .pickerStyle(.segmented)
                if minutes == 0 {
                    TextField("e.g. 90m, 6h, 2d (at most 30d)", text: $custom)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                    if !custom.isEmpty && duration == nil {
                        Text("Use minutes (m), hours (h), days (d) or weeks (w), up to 30 days.").font(.footnote).foregroundStyle(.red)
                    }
                }
            }
            Section {
                TextField("Why (required)", text: $comment, axis: .vertical).lineLimit(2...5)
            } header: {
                Text("Comment")
            } footer: {
                Text("Saved on the Alertmanager with the silence, created by ichor.")
            }
            Section {
                Button {
                    submit()
                } label: {
                    HStack {
                        Text("Silence")
                        if busy {
                            Spacer()
                            ProgressView()
                        }
                    }
                }
                .disabled(!ready)
                .kubeGated(.alertmanagerSilence)
                KubeDeniedNote(.alertmanagerSilence)
                if let failure { Text(failure).font(.footnote).foregroundStyle(.red) }
            }
        }
        .navigationTitle("Silence")
        .navigationBarTitleDisplayMode(.inline)
        .task {
            guard matchers.isEmpty else { return }
            // The fallback mutes every alert of this name: still narrow, never everything.
            matchers = (try? await TalosClient.alertmanagerSilenceMatchers(alert.labels))
                ?? [AMMatcher(name: "alertname", value: alert.alertname)]
        }
    }

    private func submit() {
        guard let duration else { return }
        busy = true
        failure = nil
        let chosen = matchers
        let why = comment.trimmingCharacters(in: .whitespacesAndNewlines)
        Task {
            let failed = await silence(chosen, duration, why)
            busy = false
            if let failed {
                failure = failed
            } else {
                done()
            }
        }
    }
}
