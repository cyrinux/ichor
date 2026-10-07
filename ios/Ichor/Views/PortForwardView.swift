import SwiftUI
import SafariServices
import UniformTypeIdentifiers
import IchorCore

/// `kubectl port-forward` to a pod: one of the ports its containers declare, or one typed. The
/// Go core listens on the phone's loopback address only (a random port), so the page opens in
/// the in-app browser and nowhere else; the forward stops with Stop or when the screen goes
/// away. Connections the pod refuses are listed while the forward goes on.
struct PortForwardView: View {
    let namespace: String
    let pod: String

    // Explicit: the private @State makes the memberwise init private.
    init(namespace: String, pod: String) {
        self.namespace = namespace
        self.pod = pod
    }

    @Environment(AppModel.self) private var model
    /// The container ports read from the pod; nil while loading.
    @State private var ports: [KubeContainerPort]?
    @State private var typedPort = ""
    /// The pod port forwarded; nil when stopped.
    @State private var active: Int?
    /// Bumped by "Start again": the same port's forward starts anew.
    @State private var runs = 0
    /// "127.0.0.1:PORT" once listening.
    @State private var address: String?
    @State private var connectionErrors: [String] = []
    /// Why the forward stopped by itself.
    @State private var ended: String?
    @State private var browsing: URL?
    @State private var message: String?

    private static let maxErrors = 20

    private var demo: Bool { model.activeSummary?.demo == true }

    var body: some View {
        List {
            Section {
                LabeledContent("Pod") { Text(verbatim: pod).monospaced().lineLimit(1).truncationMode(.middle) }
                LabeledContent("Namespace") { Text(verbatim: namespace).monospaced() }
            }
            if demo {
                Section {
                    Label("Port forwarding is not available in the demo cluster.", systemImage: "info.circle")
                        .foregroundStyle(.secondary)
                }
            } else if let active {
                forwardSection(remotePort: active)
            } else {
                portsSection
                typedSection
            }
            if let ended {
                Section {
                    Label { Text(verbatim: ended).textSelection(.enabled) } icon: { Image(systemName: "exclamationmark.triangle.fill") }
                        .foregroundStyle(.statusBad)
                } header: {
                    Text("Stopped")
                }
            }
            if !connectionErrors.isEmpty {
                Section {
                    ForEach(Array(connectionErrors.enumerated()), id: \.offset) { _, error in
                        Text(verbatim: error).font(.caption.monospaced()).foregroundStyle(.statusWarn).textSelection(.enabled)
                    }
                } header: {
                    Text("Connection errors")
                } footer: {
                    Text("The forward keeps listening: each connection is tried again.")
                }
            }
            Section {
                Text("Only this phone can open the forwarded port, and only while Ichor stays open: nothing listens on the network the phone is on.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
        .themedBackground()
        .navigationTitle(Text("Port forward"))
        .navigationBarTitleDisplayMode(.inline)
        .sheet(isPresented: $browsing.isPresent()) {
            if let browsing { SafariView(url: browsing).ignoresSafeArea() }
        }
        .messageAlert($message)
        .task(id: kubeNamespacesKey(model)) { await loadPorts() }
        // Runs while a port is forwarded and the screen shown: cancelling it stops the forward.
        .task(id: "\(active ?? 0)#\(runs)") { await forward() }
        .onDisappear { active = nil }
    }

    private func forwardSection(remotePort: Int) -> some View {
        Section {
            if let address, let url = portForwardURL(address) {
                VStack(alignment: .leading, spacing: 4) {
                    Text(verbatim: url.absoluteString)
                        .font(.title3.monospaced().weight(.semibold))
                        .textSelection(.enabled)
                    Text("Forwards to port \(String(remotePort)) of the pod")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                .padding(.vertical, 2)
                Button { browsing = url } label: { Label("Open in browser", systemImage: "safari") }
                Button { copy(url.absoluteString) } label: { Label("Copy address", systemImage: "doc.on.doc") }
            } else if ended == nil {
                HStack(spacing: 8) {
                    ProgressView()
                    Text("Starting…").foregroundStyle(.secondary)
                }
            } else {
                Button { runs += 1 } label: { Label("Start again", systemImage: "arrow.clockwise") }
            }
            Button(role: .destructive) { stop() } label: { Label("Stop", systemImage: "stop.circle") }
        } header: {
            Text("Forwarding")
        }
    }

    @ViewBuilder private var portsSection: some View {
        Section {
            if let ports {
                if ports.isEmpty {
                    Text("The containers declare no TCP port: type one below.").font(.callout).foregroundStyle(.secondary)
                }
                ForEach(ports) { port in
                    Button { start(port.port) } label: {
                        HStack {
                            Text(verbatim: "\(port.port)").font(.body.monospacedDigit())
                            if !port.name.isEmpty {
                                Text(verbatim: port.name).font(.callout).foregroundStyle(.secondary)
                            }
                            Spacer()
                            Image(systemName: "arrow.left.arrow.right.circle")
                        }
                        .contentShape(Rectangle())
                    }
                }
            } else {
                ProgressView().frame(maxWidth: .infinity)
            }
        } header: {
            Text("Container ports")
        }
    }

    private var typedSection: some View {
        Section {
            HStack {
                TextField(text: $typedPort, prompt: Text(verbatim: "8080")) { Text("Port") }
                    .keyboardType(.numberPad)
                    .font(.body.monospacedDigit())
                Button("Start") {
                    if let port = parsePort(typedPort) { start(port) }
                }
                .disabled(parsePort(typedPort) == nil)
            }
        } header: {
            Text("Another port")
        }
    }

    private func start(_ port: Int) {
        ended = nil
        connectionErrors = []
        address = nil
        active = port
    }

    private func stop() {
        active = nil
        address = nil
    }

    /// Follows the forward of `active` until it stops or the task is cancelled.
    private func forward() async {
        guard let port = active, let client = model.client else { return }
        address = nil
        ended = nil
        for await event in client.portForward(namespace: namespace, pod: pod, remotePort: port) {
            switch event {
            case .ready(let ready):
                address = ready
            case .connectionError(let error):
                connectionErrors = Array((connectionErrors + [error]).suffix(Self.maxErrors))
            case .done(let error):
                guard !Task.isCancelled else { return }
                address = nil
                ended = error ?? String(localized: "The forward stopped.")
            }
        }
    }

    /// The ports the pod's containers declare, from its YAML.
    private func loadPorts() async {
        guard let client = model.client else { return }
        let podResource = KubeAPIResource(resource: "pods", kind: "Pod")
        let yaml = try? await client.objectYAML(podResource, namespace: namespace, name: pod, reveal: false)
        guard !Task.isCancelled else { return }
        ports = containerPorts(fromPodYAML: yaml ?? "")
    }

    /// Device-only pasteboard.
    private func copy(_ text: String) {
        UIPasteboard.general.setItems([[UTType.utf8PlainText.identifier: text]], options: [.localOnly: true])
        message = String(localized: "Copied.")
    }
}

/// The in-app browser: the app stays in the foreground, so the forward keeps running.
private struct SafariView: UIViewControllerRepresentable {
    let url: URL

    func makeUIViewController(context: Context) -> SFSafariViewController {
        SFSafariViewController(url: url)
    }

    func updateUIViewController(_ controller: SFSafariViewController, context: Context) {}
}
