import SwiftUI
import IchorCore

/// The META tab of the machine config screen: the node's boot-time keys. Writing or deleting
/// one asks for the typed hostname, then Face ID / passcode when the app lock is on.
struct MetaKeysView: View {
    let node: String
    let hostname: String

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<MetaKeyList> = .loading
    @State private var editing: MetaEditTarget?
    @State private var pending: MetaChange?
    @State private var busy = false
    @State private var message: String?

    var body: some View {
        LoadStateView(state: state, retry: load) { list in
            List {
                Section {
                    Label(
                        "Boot-time values Talos reads before the machine config. A wrong value can keep the node from booting: change them only to fix a node that cannot boot with its config.",
                        systemImage: "exclamationmark.triangle.fill"
                    )
                    .font(.footnote)
                    .foregroundStyle(.statusBad)
                    if !list.loaded {
                        Text("The node has not read its META partition yet.").font(.footnote).foregroundStyle(
                            .secondary)
                    }
                    if let message {
                        Text(verbatim: message).font(.footnote)
                    }
                }
                Section {
                    if list.keys.isEmpty {
                        Text("No keys.").foregroundStyle(.secondary)
                    }
                    ForEach(list.keys) { key in row(key) }
                    Button("Add a key…") { editing = MetaEditTarget(current: nil) }
                        .disabled(busy)
                }
            }
            .overlay { if busy { ProgressView() } }
        }
        .task { await load() }
        .sheet(item: $editing) { target in
            MetaEditSheet(current: target.current, known: known) { key, value in
                editing = nil
                pending = .write(key: key, value: value)
            }
        }
        .sheet(item: $pending) { change in
            HostnameConfirmationSheet(
                title: change.title(hostname: hostname),
                message: change.isWrite
                    ? String(
                        localized: "Talos reads it at the next boot. A wrong value can keep the node from booting.")
                    : String(localized: "Talos no longer finds this value at the next boot."),
                hostname: hostname,
                actionTitle: change.isWrite ? String(localized: "Write") : String(localized: "Delete")
            ) {
                pending = nil
                Task { await apply(change) }
            }
        }
    }

    @ViewBuilder
    private func row(_ key: MetaKey) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(verbatim: key.hex).font(.headline.monospaced())
            Text(
                verbatim: [
                    key.name.isEmpty ? String(localized: "Unknown key") : key.name,
                    ByteCountFormatter.string(fromByteCount: Int64(key.size), countStyle: .file),
                ].joined(separator: "  ·  ")
            )
            .font(.caption).foregroundStyle(.secondary)
            if key.secret {
                Text("Holds a secret: not shown.").font(.caption).foregroundStyle(.secondary)
            } else {
                if key.binary {
                    Text("Binary value, shown in hex.").font(.caption).foregroundStyle(.secondary)
                }
                Text(verbatim: key.value).font(.caption.monospaced()).lineLimit(12).textSelection(.enabled)
            }
            if !key.writable {
                Text("Written by Talos only.").font(.caption).foregroundStyle(.secondary)
            }
        }
        .swipeActions {
            if key.writable {
                Button("Delete", role: .destructive) { pending = .delete(key: key.key) }
            }
            if key.editable {
                Button("Edit") { editing = MetaEditTarget(current: key) }
            }
        }
        .contextMenu {
            if !key.value.isEmpty {
                Button {
                    UIPasteboard.general.string = key.value
                } label: {
                    Label("Copy", systemImage: "doc.on.doc")
                }
            }
            if key.editable {
                Button {
                    editing = MetaEditTarget(current: key)
                } label: {
                    Label("Edit", systemImage: "pencil")
                }
            }
            if key.writable {
                Button(role: .destructive) {
                    pending = .delete(key: key.key)
                } label: {
                    Label("Delete", systemImage: "trash")
                }
            }
        }
    }

    private var known: [MetaKey] {
        if case .loaded(let list, _, _) = state { return list.known }
        return []
    }

    private func load() async {
        guard let client = model.client else { return }
        state = await LoadState.from { try await client.metaKeys(node: node) }
    }

    private func apply(_ change: MetaChange) async {
        guard let client = model.client, !busy else { return }
        if model.lock.enabled,
            let failure = await Authenticator.authenticate(reason: String(localized: "Change META on \(hostname)"))
        {
            message = failure
            return
        }
        busy = true
        do {
            switch change {
            case .write(let key, let value):
                try await client.metaWrite(node: node, key: key, value: value)
                message = String(localized: "\(metaHex(key)) written")
            case .delete(let key):
                try await client.metaDelete(node: node, key: key)
                message = String(localized: "\(metaHex(key)) deleted")
            }
        } catch {
            message = error.localizedDescription
        }
        busy = false
        await load()
    }
}

private struct MetaEditTarget: Identifiable {
    let current: MetaKey?
    var id: Int { current?.key ?? -1 }
}

private enum MetaChange: Identifiable {
    case write(key: Int, value: String)
    case delete(key: Int)

    var id: String {
        switch self {
        case .write(let key, _): "write-\(key)"
        case .delete(let key): "delete-\(key)"
        }
    }

    var isWrite: Bool {
        if case .write = self { return true }
        return false
    }

    func title(hostname: String) -> String {
        switch self {
        case .write(let key, _): String(localized: "Write \(metaHex(key)) on \(hostname)?")
        case .delete(let key): String(localized: "Delete \(metaHex(key)) from \(hostname)?")
        }
    }
}

/// Typing a key's value: `current` when editing one, else a key picked from `known` or typed in hex.
private struct MetaEditSheet: View {
    let current: MetaKey?
    let known: [MetaKey]
    let onWrite: (Int, String) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var keyText: String
    @State private var value: String

    init(current: MetaKey?, known: [MetaKey], onWrite: @escaping (Int, String) -> Void) {
        self.current = current
        self.known = known
        self.onWrite = onWrite
        _keyText = State(initialValue: current?.hex ?? "")
        _value = State(initialValue: current?.value ?? "")
    }

    private var key: Int? { parseMetaKey(keyText) }
    private var knownKey: MetaKey? { known.first { $0.key == key } }
    private var writable: Bool { key.map { $0 != 0 && knownKey?.writable != false } ?? false }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("Key (hex)", text: $keyText)
                        .font(.body.monospaced())
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .disabled(current != nil)
                    if !keyText.isEmpty && key == nil {
                        Text("A key from 0x00 to 0xff.").font(.caption).foregroundStyle(.statusBad)
                    } else if key != nil && !writable {
                        Text("Written by Talos only.").font(.caption).foregroundStyle(.statusBad)
                    } else if let knownKey {
                        Text(verbatim: knownKey.name).font(.caption).foregroundStyle(.secondary)
                    }
                    if current == nil {
                        Picker("Key (hex)", selection: $keyText) {
                            Text(verbatim: "—").tag(keyText)
                            ForEach(known.filter(\.writable)) { k in
                                Text(verbatim: "\(k.hex) \(k.name)").tag(k.hex)
                            }
                        }
                    }
                }
                Section("Value") {
                    TextEditor(text: $value)
                        .font(.caption.monospaced())
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .frame(minHeight: 120)
                }
            }
            .navigationTitle("META key")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Write…") { if let key { onWrite(key, value) } }
                        .disabled(!writable || value.isEmpty)
                }
            }
        }
    }
}
