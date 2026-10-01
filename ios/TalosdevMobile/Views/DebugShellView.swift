import SwiftTerm
import SwiftUI
import Talosmobile
import TalosdevMobileCore

/// `talosctl debug -n NODE IMAGE --args ARGS`: a privileged container with a terminal
/// (SwiftTerm; its keyboard accessory adds Esc, Ctrl, Tab and arrows).
struct DebugShellView: View {
    let node: String
    let hostname: String

    init(node: String, hostname: String) {
        self.node = node
        self.hostname = hostname
    }

    @Environment(AppModel.self) private var model
    @AppStorage("debug.image") private var image = "nicolaka/netshoot:latest"
    @AppStorage("debug.args") private var args = "/bin/sh"
    @State private var shell = DebugShell()
    @State private var error: String?

    var body: some View {
        Group {
            switch shell.state {
            case .setup:
                Form {
                    Section {
                        Text("Runs the image as a privileged container on \(hostname), with host access, and opens a terminal in it. Talos pulls the image first if the node does not have it.")
                            .font(.callout)
                    }
                    Section("Image") {
                        TextField("Image", text: $image).font(.body.monospaced())
                            .autocorrectionDisabled().textInputAutocapitalization(.never)
                    }
                    Section("Command") {
                        TextField("Command", text: $args).font(.body.monospaced())
                            .autocorrectionDisabled().textInputAutocapitalization(.never)
                    }
                    if let error { Text(error).foregroundStyle(.red) }
                    Button("Start shell") { Task { await start() } }
                        .disabled(image.trimmingCharacters(in: .whitespaces).isEmpty)
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
        .navigationTitle("Debug shell")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if shell.isActive {
                ToolbarItem(placement: .primaryAction) { Button("Stop") { shell.stop() } }
            }
        }
        .onDisappear { shell.stop() }
    }

    /// With the app lock on, a privileged shell needs a fresh Face ID / passcode, like reboot.
    private func start() async {
        guard let client = model.client else { return }
        if model.lock.enabled, let failure = await Authenticator.authenticate(reason: String(localized: "Debug shell on \(hostname)")) {
            error = failure
            return
        }
        error = nil
        shell.start(client: client, node: node, image: image, args: args)
    }
}

/// Owns the Go session and the terminal view; Go callbacks hop to the main actor.
@Observable
@MainActor
final class DebugShell {
    enum State: Equatable {
        case setup
        case starting(String)
        case running
        case exited(code: Int, message: String)
    }

    private(set) var state = State.setup
    let terminal = TerminalView(frame: .zero)
    private var session: TalosmobileDebugSession?
    private var bridge: Bridge?

    var isActive: Bool {
        switch state {
        case .starting, .running: true
        default: false
        }
    }

    var statusLine: String? {
        switch state {
        case .starting(let status): status
        case .exited(let code, let message): code >= 0 ? String(localized: "Exited with code \(code)") : message
        default: nil
        }
    }

    func start(client: TalosClient, node: String, image: String, args: String) {
        stop()
        terminal.getTerminal().resetToInitialState()
        state = .starting(String(localized: "Connecting…"))
        let bridge = Bridge(owner: self)
        self.bridge = bridge
        let terminal = terminal.getTerminal()
        session = client.startDebugShell(node: node, image: image, args: args,
                                         cols: max(terminal.cols, 20), rows: max(terminal.rows, 5), listener: bridge)
    }

    func write(_ bytes: ArraySlice<UInt8>) { session?.write(Data(bytes)) }
    func resize(cols: Int, rows: Int) { session?.resize(cols, rows: rows) }

    func stop() {
        session?.close()
        session = nil
    }

    func reset() {
        stop()
        state = .setup
    }

    fileprivate func receive(_ data: Data) {
        if state != .running { state = .running }
        terminal.feed(byteArray: ArraySlice([UInt8](data)))
    }

    fileprivate func status(_ message: String) { state = .starting(message) }
    fileprivate func exited(_ code: Int, _ message: String) { state = .exited(code: code, message: message) }

    /// gomobile protocol (a class of the same name exists, hence "Protocol").
    private final class Bridge: NSObject, TalosmobileDebugListenerProtocol, @unchecked Sendable {
        weak var owner: DebugShell?
        init(owner: DebugShell) { self.owner = owner }

        func onStatus(_ message: String?) {
            let text = message ?? ""
            Task { @MainActor [weak owner] in owner?.status(text) }
        }

        func onOutput(_ data: Data?) {
            guard let data else { return }
            Task { @MainActor [weak owner] in owner?.receive(data) }
        }

        func onExit(_ code: Int, errMessage: String?) {
            let message = errMessage ?? ""
            Task { @MainActor [weak owner] in owner?.exited(code, message) }
        }
    }
}

private struct TerminalHost: UIViewRepresentable {
    let shell: DebugShell

    func makeCoordinator() -> Coordinator { Coordinator(shell: shell) }

    func makeUIView(context: Context) -> TerminalView {
        let view = shell.terminal
        view.terminalDelegate = context.coordinator
        view.nativeBackgroundColor = UIColor(red: 0.04, green: 0.07, blue: 0.13, alpha: 1)
        view.nativeForegroundColor = UIColor(red: 0.89, green: 0.92, blue: 0.95, alpha: 1)
        view.becomeFirstResponder()
        return view
    }

    func updateUIView(_ view: TerminalView, context: Context) {}

    final class Coordinator: NSObject, TerminalViewDelegate {
        let shell: DebugShell
        init(shell: DebugShell) { self.shell = shell }

        func send(source: TerminalView, data: ArraySlice<UInt8>) {
            MainActor.assumeIsolated { shell.write(data) }
        }

        func sizeChanged(source: TerminalView, newCols: Int, newRows: Int) {
            MainActor.assumeIsolated { shell.resize(cols: newCols, rows: newRows) }
        }

        func setTerminalTitle(source: TerminalView, title: String) {}
        func hostCurrentDirectoryUpdate(source: TerminalView, directory: String?) {}
        func scrolled(source: TerminalView, position: Double) {}
        func requestOpenLink(source: TerminalView, link: String, params: [String: String]) {}
        func clipboardCopy(source: TerminalView, content: Data) {
            UIPasteboard.general.string = String(data: content, encoding: .utf8)
        }
        func rangeChanged(source: TerminalView, startY: Int, endY: Int) {}
    }
}
