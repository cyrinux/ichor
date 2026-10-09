import SwiftUI
import Ichorgo

/// Where the node debug pod is created, and the setting that offers it.
enum NodeDebugKeys {
    static let enabled = "nodeDebugShellEnabled"
    static let namespace = "default"
}

/// `kubectl debug node/NODE` on a cluster without Talos: a privileged pod on the node, in the
/// host's namespaces, then nsenter for a root shell on the node. The pod is deleted when the
/// shell ends, which is when the view goes, like the Talos debug shell.
struct KubeNodeDebugView: View {
    let node: String

    @Environment(AppModel.self) private var model
    @AppStorage("debug.nodeImage") private var image = "busybox:1.37"
    @State private var shell = DebugShell()
    @State private var error: String?

    private let namespace = NodeDebugKeys.namespace

    var body: some View {
        Group {
            switch shell.state {
            case .setup:
                Form {
                    Section {
                        Text("A privileged pod runs on \(node) in namespace \(namespace), in the node’s own namespaces, and nsenter opens a root shell on the node. The pod is deleted when the shell ends. The namespace must admit privileged pods.")
                            .font(.callout)
                        KubeDeniedNote(.debugNode, in: namespace)
                    }
                    Section("Image") {
                        TextField("Image", text: $image).font(.body.monospaced())
                            .autocorrectionDisabled().textInputAutocapitalization(.never)
                    }
                    if let error { Text(error).foregroundStyle(.statusBad) }
                    Button("Start shell") { Task { await start() } }
                        .disabled(image.trimmingCharacters(in: .whitespaces).isEmpty)
                        .kubeGated(.debugNode, in: namespace)
                }
            default:
                VStack(spacing: 0) {
                    if let status = shell.statusLine {
                        HStack {
                            Text(status).font(.footnote)
                            Spacer()
                            if case .exited = shell.state { Button("New shell") { shell.reset() } }
                        }
                        .padding(.horizontal).padding(.vertical, 6)
                        .background(.bar)
                    }
                    TerminalHost(shell: shell)
                }
                .background(Color(red: 0.04, green: 0.07, blue: 0.13))
            }
        }
        .navigationTitle(Text(verbatim: node))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if shell.isActive {
                ToolbarItem(placement: .primaryAction) { Button("Stop") { shell.stop() } }
            }
        }
        .loadsKubeActionAccess(namespace: namespace)
        .onDisappear { shell.stop() }
    }

    /// Root on the node: with the app lock on, a fresh Face ID first, like a Talos shell.
    private func start() async {
        guard let client = model.client else { return }
        if model.lock.enabled, let failure = await Authenticator.authenticate(reason: String(localized: "Root shell on \(node)")) {
            error = failure
            return
        }
        error = nil
        let (node, namespace, image) = (node, namespace, image.trimmingCharacters(in: .whitespaces))
        shell.start { listener, cols, rows in
            client.startNodeDebug(node: node, namespace: namespace, image: image, cols: cols, rows: rows, listener: listener)
        }
    }
}
