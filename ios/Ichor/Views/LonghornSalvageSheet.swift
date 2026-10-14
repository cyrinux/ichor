import SwiftUI
import IchorCore

struct LonghornSalvageSheet: View {
    let volume: LonghornVolume
    let onConfirm: (Set<String>) -> Void
    @Environment(AppModel.self) private var model
    @Environment(\.kubeActionAccess) private var access
    @Environment(\.dismiss) private var dismiss
    @State private var preview: LonghornSalvagePreview?
    @State private var selection: Set<String> = []
    @State private var typed = ""
    @State private var error: String?

    init(volume: LonghornVolume, onConfirm: @escaping (Set<String>) -> Void) {
        self.volume = volume
        self.onConfirm = onConfirm
    }
    var body: some View {
        let denial = access?.denial(for: .longhornSalvage, in: volume.namespace)
        NavigationStack {
            Form {
                Section {
                    Text("Salvage may lose data. Choose the replicas whose data you want to recover; their timestamps do not guarantee consistency.").foregroundStyle(.statusWarn)
                    Text("Choose replicas explicitly. No replica is selected automatically.")
                }
                if preview == nil && error == nil { ProgressView() }
                if let error { ErrorLine(error: error) }
                if let p = preview {
                    Section {
                        Text(verbatim: "\(p.state) · \(p.robustness)")
                        if !p.canSalvage { Text("Salvage requires a detached faulted volume and available replicas.") }
                        if !p.error.isEmpty { ErrorLine(error: p.error) }
                        ForEach(p.replicas) { replica in
                            Toggle(isOn: Binding(get: { selection.contains(replica.name) }, set: { checked in
                                if checked { selection.insert(replica.name) } else { selection.remove(replica.name) }
                            })) {
                                Text("\(replica.name) on \(replica.node) · healthy: \(relativeTime(replica.healthyAt)) · failed: \(relativeTime(replica.failedAt))")
                            }.disabled(!replica.eligible)
                        }
                    }
                }
                Section("Type \(volume.name) to confirm") {
                    TextField("Volume name", text: $typed).autocorrectionDisabled().textInputAutocapitalization(.never)
                    Button { onConfirm(selection) } label: { Text("Salvage volume") }
                        .disabled(preview?.accepts(selection) != true || !typedConfirmationMatches(typed, token: volume.name) || denial != nil)
                    KubeDeniedNote(.longhornSalvage, in: volume.namespace)
                }
            }
            .navigationTitle(Text("Salvage volume"))
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } } }
        }
        .task(id: volume.id) {
            guard let client = model.client else { error = String(localized: "Not connected"); return }
            do { preview = try await client.longhornSalvagePreview(namespace: volume.namespace, name: volume.name) }
            catch { self.error = error.localizedDescription }
        }
    }
}
