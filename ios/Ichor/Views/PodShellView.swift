import SwiftUI
import Ichorgo

/// `kubectl exec -it -n NAMESPACE POD -c CONTAINER -- COMMAND`: a terminal in a running
/// container, through the Kubernetes API. An empty command runs bash, or sh when the image
/// lacks it. The shell ends when the view goes, like the node's debug shell.
struct PodShellView: View {
    let namespace: String
    let pod: String
    /// "" lets Kubernetes pick the pod's only container.
    let container: String

    @Environment(AppModel.self) private var model
    @AppStorage("debug.podCommand") private var command = ""
    @State private var shell = DebugShell()
    @State private var error: String?

    var body: some View {
        Group {
            switch shell.state {
            case .setup:
                Form {
                    Section {
                        Text("Opens a terminal in the running container of \(pod), like kubectl exec -it. Leave the command empty to run bash, or sh when the image has no bash.")
                            .font(.callout)
                    }
                    Section("Command") {
                        TextField("bash, or sh", text: $command).font(.body.monospaced())
                            .autocorrectionDisabled().textInputAutocapitalization(.never)
                    }
                    if let error { Text(error).foregroundStyle(.statusBad) }
                    Button("Start shell") { Task { await start() } }
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
        .navigationTitle(Text(verbatim: container.isEmpty ? pod : "\(pod) / \(container)"))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if shell.isActive {
                ToolbarItem(placement: .primaryAction) { Button("Stop") { shell.stop() } }
            }
        }
        .onDisappear { shell.stop() }
    }

    /// A shell can read the container's secrets: with the app lock on, it asks like the node's.
    private func start() async {
        guard let client = model.client else { return }
        if model.lock.enabled, let failure = await Authenticator.authenticate(reason: String(localized: "Shell in \(pod)")) {
            error = failure
            return
        }
        error = nil
        let (namespace, pod, container, command) = (namespace, pod, container, command)
        shell.start { listener, cols, rows in
            client.startPodShell(namespace: namespace, pod: pod, container: container, command: command,
                                 cols: cols, rows: rows, listener: listener)
        }
    }
}
