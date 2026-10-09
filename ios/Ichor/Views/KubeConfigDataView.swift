import SwiftUI
import UniformTypeIdentifiers
import IchorCore

/// The data tab of a Secret or ConfigMap: each key with its size and what it looks like, its
/// certificate's expiry, the value on a tap, a docker config's registries and the pods using
/// it (each opens). A Secret's value is read one key at a time, behind Face ID / the passcode
/// when the app lock is on, and only one stays shown; a ConfigMap's values need no lock (the
/// YAML shows them too). Values are never logged; a Secret's copy expires from the pasteboard.
struct KubeConfigDataView: View {
    let resource: KubeAPIResource
    let namespace: String
    let name: String

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<KubeConfigData> = .loading
    /// The one Secret key whose value is shown.
    @State private var revealed: ConfigDataKey?
    /// The Secret key being read.
    @State private var revealing: String?
    /// The ConfigMap keys opened.
    @State private var expanded: Set<String> = []
    @State private var message: String?

    /// A value longer than this is cut on screen (Copy still takes all of it).
    private static let previewChars = 20_000

    var body: some View {
        LoadStateView(state: state, retry: load) { data in
            List { sections(data) }
                .themedBackground()
                .refreshable { await load() }
        }
        .messageAlert($message)
        .task(id: kubeNamespacesKey(model)) { await load() }
        .onDisappear { revealed = nil }
    }

    @ViewBuilder private func sections(_ data: KubeConfigData) -> some View {
        let now = Int64(Date().timeIntervalSince1970)
        Section {
            if !data.type.isEmpty {
                Text("Type: \(data.type)").font(.caption).foregroundStyle(.secondary)
            }
            if revealed != nil {
                Label("Secret values are shown in clear. Don't share screenshots.", systemImage: "exclamationmark.triangle.fill")
                    .font(.footnote)
                    .foregroundStyle(.statusWarn)
            }
            if data.keys.isEmpty {
                Text("No keys.").font(.caption).foregroundStyle(.secondary)
            }
            ForEach(data.keys(with: revealed)) { key in
                keyRow(key, now: now)
            }
        } header: {
            Text("Keys")
        }
        if !data.registries.isEmpty {
            Section {
                ForEach(data.registries) { registry in
                    VStack(alignment: .leading, spacing: 2) {
                        Text(verbatim: registry.registry).font(.subheadline.monospaced()).lineLimit(1)
                        if !registry.username.isEmpty {
                            Text("User: \(registry.username)").font(.caption).foregroundStyle(.secondary)
                        }
                    }
                }
            } header: {
                Text("Registries")
            }
        }
        Section {
            usedBy(data)
        } header: {
            Text("Used by")
        }
    }

    private func isShown(_ key: ConfigDataKey) -> Bool {
        resource.isSecret ? key.revealed : expanded.contains(key.key)
    }

    @ViewBuilder private func keyRow(_ key: ConfigDataKey, now: Int64) -> some View {
        let shown = isShown(key)
        VStack(alignment: .leading, spacing: 6) {
            Button { Task { await toggle(key) } } label: {
                HStack(spacing: 8) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(verbatim: key.key).font(.subheadline.monospaced()).lineLimit(2)
                        Text(verbatim: facts(key)).font(.caption).foregroundStyle(.secondary)
                    }
                    Spacer()
                    if revealing == key.key {
                        ProgressView()
                    } else {
                        Image(systemName: resource.isSecret ? (shown ? "eye.slash" : "eye") : (shown ? "chevron.up" : "chevron.down"))
                            .foregroundStyle(.secondary)
                            .accessibilityHidden(true)
                    }
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .disabled(revealing == key.key)
            .accessibilityHint(shown ? Text("Hide value") : Text("Show value"))
            if let cert = key.cert {
                certLines(cert, now: now)
            }
            if shown && key.revealed {
                valueBlock(key)
            }
        }
        .padding(.vertical, 2)
    }

    /// "1.2 KiB · JSON".
    private func facts(_ key: ConfigDataKey) -> String {
        let hint = switch key.hint {
        case ConfigDataKey.hintJSON: "JSON"
        case ConfigDataKey.hintPEM: "PEM"
        case ConfigDataKey.hintBinary: String(localized: "Binary")
        default: String(localized: "Text")
        }
        return "\(formatBytes(key.size)) · \(hint)"
    }

    /// The certificate's expiry, tinted when near or past, and whom it is for.
    @ViewBuilder private func certLines(_ cert: ConfigDataCert, now: Int64) -> some View {
        let tone = cert.tone(now: now)
        Text(verbatim: "\(String(localized: "Cert expires")): \(localizedCertExpiry(cert.notAfter))")
            .font(.caption)
            .foregroundStyle((tone == .good ? nil : tone.color) ?? .secondary)
        if !cert.names.isEmpty {
            Text(verbatim: cert.names.joined(separator: ", ")).font(.caption).foregroundStyle(.secondary).lineLimit(2)
        }
        if cert.count > 1 {
            Text("Certificates in the chain: \(cert.count)").font(.caption).foregroundStyle(.secondary)
        }
    }

    /// The value, selectable, cut when huge; Copy takes all of it.
    @ViewBuilder private func valueBlock(_ key: ConfigDataKey) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            if key.base64 {
                Text("Binary value, shown as base64.").font(.caption).foregroundStyle(.secondary)
            }
            Text(verbatim: key.value.count > Self.previewChars ? String(key.value.prefix(Self.previewChars)) + "…" : key.value)
                .font(.caption.monospaced())
                .textSelection(.enabled)
            Button { copy(key) } label: {
                Label("Copy value", systemImage: "doc.on.doc")
            }
            .buttonStyle(.borderless)
            .font(.caption)
        }
    }

    @ViewBuilder private func usedBy(_ data: KubeConfigData) -> some View {
        if data.usedByUnknown {
            Text("The pods of this namespace could not be listed.").font(.caption).foregroundStyle(.secondary)
        } else if data.usedBy.isEmpty {
            Text("No pod of this namespace uses it.").font(.caption).foregroundStyle(.secondary)
        } else {
            ForEach(data.usedBy) { use in
                NavigationLink {
                    KubeObjectView(resource: KubeAPIResource(resource: "pods", kind: "Pod"), namespace: namespace, name: use.pod)
                } label: {
                    VStack(alignment: .leading, spacing: 3) {
                        Text(verbatim: use.pod).font(.subheadline.monospaced()).lineLimit(1)
                        // Kubernetes' field names: env, envFrom, volume, projected, imagePullSecret.
                        Text(verbatim: use.via.joined(separator: " · ")).font(.caption.monospaced()).foregroundStyle(.secondary)
                    }
                }
            }
        }
    }

    /// The keys and the pods using the object; a value shown leaves the screen.
    private func load() async {
        guard let client = model.client else { return }
        let result: LoadState<KubeConfigData> = await .from {
            try await client.configData(resource, namespace: namespace, name: name, key: "")
        }
        revealed = nil
        state = state.refreshed(with: result)
    }

    /// Opens or closes a key: a ConfigMap's at once, a Secret's after Face ID / the passcode
    /// (app lock on), reading that one value.
    private func toggle(_ key: ConfigDataKey) async {
        guard resource.isSecret else {
            if expanded.contains(key.key) { expanded.remove(key.key) } else { expanded.insert(key.key) }
            return
        }
        if key.revealed {
            revealed = nil
            return
        }
        if model.lock.enabled,
           let failure = await Authenticator.authenticate(reason: String(localized: "Show \(key.key) of \(name)")) {
            message = failure
            return
        }
        guard let client = model.client else { return }
        revealed = nil
        revealing = key.key
        defer { revealing = nil }
        do {
            let data = try await client.configData(resource, namespace: namespace, name: name, key: key.key)
            revealed = data.keys.first { $0.key == key.key && $0.revealed }
        } catch {
            message = error.localizedDescription
        }
    }

    /// Device-only pasteboard; a Secret's value expires from it after two minutes.
    private func copy(_ key: ConfigDataKey) {
        let secret = resource.isSecret
        let options: [UIPasteboard.OptionsKey: Any] = secret
            ? [.localOnly: true, .expirationDate: Date().addingTimeInterval(120)]
            : [.localOnly: true]
        UIPasteboard.general.setItems([[UTType.utf8PlainText.identifier: key.value]], options: options)
        message = secret ? String(localized: "Copied. The clipboard is cleared in 2 minutes.") : String(localized: "Copied.")
    }
}
