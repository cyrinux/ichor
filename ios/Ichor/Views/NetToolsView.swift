import SwiftUI
import IchorCore

/// Network checks run from a node in a privileged netshoot container (os:admin, like the debug
/// shell): DNS, ping, a TCP port, the path, an HTTP(S) URL. One at a time; each result reads as
/// fields, with the raw output underneath.
struct NetToolsView: View {
    let node: String
    let hostname: String

    @Environment(AppModel.self) private var model
    @State private var tool = NetTool.dns
    @State private var target = ""
    @State private var record = "A"
    @State private var server = ""
    @State private var count = 3
    @State private var recent: [String] = []
    @State private var lines: [String] = []
    @State private var result: NetToolResult?
    @State private var failure: String?
    @State private var task: Task<Void, Never>?

    private var running: Bool { task != nil }
    private var problem: NetTargetProblem? { netTargetProblem(tool, target) }

    var body: some View {
        List {
            if running || result != nil || failure != nil {
                runSections
            } else {
                form
            }
        }
        .themedBackground()
        .navigationTitle("Network tools")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if let result {
                ToolbarItem(placement: .primaryAction) {
                    ShareLink(item: netToolShareText(result, hostname: hostname)) { Label("Share result", systemImage: "square.and.arrow.up") }
                }
            }
        }
        .onAppear { recent = Self.loadRecent(fingerprint) }
        .onDisappear { task?.cancel() }
    }

    @ViewBuilder private var form: some View {
        Section {
            Text("Checks run from this node: what it resolves, reaches and sees on the way.").font(.footnote).foregroundStyle(.secondary)
            Picker("Tool", selection: $tool) {
                ForEach(NetTool.allCases) { Text(label($0)).tag($0) }
            }
            .pickerStyle(.segmented)
            TextField(text: $target, prompt: Text(verbatim: placeholder)) { Text("Target") }
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .keyboardType(.URL)
                .font(.body.monospaced())
                .onSubmit(start)
            if let problem, !target.isEmpty {
                Text(problemText(problem)).font(.footnote).foregroundStyle(.statusBad)
            }
            if !recent.isEmpty {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack {
                        ForEach(recent, id: \.self) { r in
                            Button { target = r } label: { Text(verbatim: r).font(.caption.monospaced()) }
                                .buttonStyle(.bordered)
                        }
                    }
                }
            }
        }
        if tool == .dns {
            Section {
                Picker("Record type", selection: $record) {
                    ForEach(netDnsRecords, id: \.self) { Text(verbatim: $0).tag($0) }
                }
                TextField(text: $server, prompt: Text(verbatim: "10.96.0.10")) { Text("Resolver") }
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .keyboardType(.numbersAndPunctuation)
            } footer: {
                Text("An IP; empty for the node's own resolver.")
            }
        }
        if tool == .ping {
            Section {
                Picker("Packets", selection: $count) {
                    ForEach([3, 5, 10], id: \.self) { Text(verbatim: "\($0)").tag($0) }
                }
                .pickerStyle(.segmented)
            }
        }
        Section {
            Button(action: start) { Text("Run").fontWeight(.semibold).frame(maxWidth: .infinity) }
                .disabled(problem != nil)
        } footer: {
            Text("Runs in a privileged netshoot container, like the debug shell (os:admin). The first run pulls the image.")
        }
    }

    @ViewBuilder private var runSections: some View {
        Section {
            Text(verbatim: "\(label(tool)) · \(target.trimmingCharacters(in: .whitespaces))").font(.headline)
            if running {
                ProgressView().frame(maxWidth: .infinity)
            }
            if let failure {
                Text(verbatim: failure.isEmpty ? String(localized: "The check ended without a result.") : failure).foregroundStyle(.statusBad)
            }
        }
        if let result {
            NetToolResultSections(result: result)
            if !result.raw.isEmpty {
                Section {
                    DisclosureGroup("Raw output") {
                        ScrollView(.horizontal) { Text(verbatim: result.raw).font(.caption.monospaced()).textSelection(.enabled) }
                    }
                }
            }
        } else if !lines.isEmpty {
            Section {
                Text(verbatim: lines.suffix(30).joined(separator: "\n")).font(.caption.monospaced())
            }
        }
        Section {
            if running {
                Button("Cancel", role: .cancel) {
                    task?.cancel()
                    task = nil
                    lines = []
                }
            } else {
                Button("New check") {
                    result = nil
                    failure = nil
                    lines = []
                }
            }
        }
    }

    private func start() {
        guard problem == nil, !running, let client = model.client else { return }
        recent = rememberNetTarget(recent, target)
        Self.saveRecent(recent, fingerprint)
        lines = []
        result = nil
        failure = nil
        let events = client.netTool(node: node, tool: tool, target: target,
                                    record: tool == .dns ? record : "", server: tool == .dns ? server : "",
                                    count: tool == .ping ? count : 0)
        task = Task {
            for await event in events {
                switch event {
                case .line(let line): lines = Array((lines + [line]).suffix(200))
                case .done(let r): result = r
                case .failed(let message): failure = message
                }
            }
            if !Task.isCancelled, result == nil, failure == nil { failure = "" }
            task = nil
        }
    }

    private var fingerprint: String { model.activeSummary?.fingerprint ?? "" }

    private var placeholder: String {
        switch tool {
        case .dns: "kubernetes.default.svc.cluster.local"
        case .ping, .trace: String(localized: "host name or IP")
        case .port: String(localized: "host:port")
        case .http: String(localized: "https://host/path")
        }
    }

    private func label(_ tool: NetTool) -> String {
        switch tool {
        case .dns: String(localized: "DNS")
        case .ping: String(localized: "Ping")
        case .port: String(localized: "Port")
        case .trace: String(localized: "Trace")
        case .http: String(localized: "HTTP/TLS")
        }
    }

    private func problemText(_ problem: NetTargetProblem) -> String {
        switch problem {
        case .empty: String(localized: "Type a target.")
        case .characters: String(localized: "No spaces, shell characters or leading dash.")
        case .hostPort: String(localized: "Give a host and a port, like 10.0.0.2:6443.")
        case .url: String(localized: "Give an http:// or https:// URL.")
        case .host: String(localized: "Give a host name or an IP, not a URL.")
        }
    }

    // The recent targets, per cluster, on this device only.
    private static func key(_ fingerprint: String) -> String { "netTools.targets.\(fingerprint)" }

    private static func loadRecent(_ fingerprint: String) -> [String] {
        guard !fingerprint.isEmpty else { return [] }
        return UserDefaults.standard.stringArray(forKey: key(fingerprint)) ?? []
    }

    private static func saveRecent(_ recent: [String], _ fingerprint: String) {
        guard !fingerprint.isEmpty else { return }
        UserDefaults.standard.set(recent, forKey: key(fingerprint))
    }
}
