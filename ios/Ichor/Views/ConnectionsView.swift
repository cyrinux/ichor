import SwiftUI
import IchorCore

/// TCP/UDP sockets of a node's host network namespace, like `talosctl netstat -a -p`
/// (os:reader): listening ones by default, searchable by address, port, process or pid.
struct ConnectionsView: View {
    let node: String
    let hostname: String

    // Explicit: the private @State properties make the memberwise init private.
    init(node: String, hostname: String) {
        self.node = node
        self.hostname = hostname
    }

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<[NodeConnection]> = .loading
    @State private var filter = ConnectionFilter.listening
    @State private var query = ""

    var body: some View {
        LoadStateView(state: state, retry: load) { connections in
            let shown = filterConnections(connections, filter: filter, query: query)
            List {
                Section {
                    Picker("Show", selection: $filter) {
                        ForEach(ConnectionFilter.allCases, id: \.self) { Text($0.localizedLabel).tag($0) }
                    }
                    .pickerStyle(.segmented)
                } footer: {
                    Text("\(shown.count) of \(connections.count) sockets")
                }
                ForEach(shown) { ConnectionRow(connection: $0) }
            }
            .emptyOverlay(shown.isEmpty, query: query) { ContentUnavailableView("No sockets", systemImage: "network.slash") }
            .refreshable { await load() }
            .themedBackground()
        }
        .searchable(text: $query, prompt: Text("Address, port, process or PID"))
        .navigationTitle(String(localized: "Connections · \(hostname)"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
    }

    private func load() async {
        guard let client = model.client else { return }
        state = await .from { try await client.connections(node: node) }
    }
}

private struct ConnectionRow: View {
    let connection: NodeConnection

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(alignment: .firstTextBaseline) {
                Text(verbatim: connection.localEndpoint)
                    .font(.body.monospaced())
                    .lineLimit(1)
                    .truncationMode(.middle)
                Spacer()
                Text(verbatim: connection.protocol.uppercased())
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(.secondary)
            }
            HStack(spacing: 12) {
                Text(verbatim: connection.state)
                    .foregroundStyle(connection.listening ? Color.green : Color.secondary)
                if !connection.listening {
                    Text(verbatim: "→ \(connection.remoteEndpoint)").lineLimit(1).truncationMode(.middle)
                }
            }
            .font(.caption.monospaced())
            if connection.pid > 0 || !connection.processName.isEmpty {
                Text(verbatim: process)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
    }

    private var process: String {
        let name = connection.processName.isEmpty ? "?" : connection.processName
        return connection.pid > 0 ? "\(name) (\(connection.pid))" : name
    }
}
