import SwiftUI
import Ichorgo

/// `virtctl console -n NAMESPACE NAME`: the serial console of a running KubeVirt virtual machine
/// in the app's terminal, through the Kubernetes API. Nothing to set up: a hint, then the
/// terminal. The console ends when the view goes, like a pod's shell.
struct VMConsoleView: View {
    let namespace: String
    let name: String

    @Environment(AppModel.self) private var model
    @State private var shell = DebugShell()
    @State private var error: String?

    private var target: String { "\(namespace)/\(name)" }

    var body: some View {
        Group {
            switch shell.state {
            case .setup:
                Form {
                    Section {
                        Text("Opens the serial console of \(target), like virtctl console.")
                            .font(.callout)
                    } footer: {
                        Text("The guest must send its console to ttyS0 (or hvc0) for anything to show. One viewer at a time: opening it elsewhere disconnects this one.")
                    }
                    KubeDeniedNote(.vmConsole, in: namespace)
                    if let error { Text(error).foregroundStyle(.statusBad) }
                    Button("Connect") { Task { await start() } }
                        .kubeGated(.vmConsole, in: namespace)
                }
            default:
                VStack(spacing: 0) {
                    if let status = shell.statusLine {
                        HStack {
                            Text(status).font(.footnote)
                            Spacer()
                            if case .exited = shell.state { Button("Reconnect") { shell.reset() } }
                        }
                        .padding(.horizontal).padding(.vertical, 6)
                        .background(.bar)
                    }
                    TerminalHost(shell: shell)
                }
                .background(Color(red: 0.04, green: 0.07, blue: 0.13))
            }
        }
        .navigationTitle(Text(verbatim: target))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if shell.isActive {
                ToolbarItem(placement: .primaryAction) { Button("Stop") { shell.stop() } }
            }
        }
        .loadsKubeActionAccess(namespace: namespace)
        .onDisappear { shell.stop() }
    }

    /// The console may sit at a logged-in shell: with the app lock on, it asks like a pod's.
    private func start() async {
        guard let client = model.client else { return }
        if model.lock.enabled, let failure = await Authenticator.authenticate(reason: String(localized: "Serial console of \(name)")) {
            error = failure
            return
        }
        error = nil
        let (namespace, name) = (namespace, name)
        shell.start { listener, _, _ in
            client.startVMConsole(namespace: namespace, name: name, listener: listener)
        }
    }
}
