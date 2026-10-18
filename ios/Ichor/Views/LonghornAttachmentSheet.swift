import SwiftUI
import IchorCore
struct LonghornAttachmentTarget: Identifiable {
    let volume: LonghornVolume
    let attach: Bool
    var id: String { volume.id }
}
struct LonghornAttachmentSheet: View {
    let target: LonghornAttachmentTarget
    let onConfirm: (String) -> Void
    @Environment(AppModel.self) private var model
    @Environment(\.kubeActionAccess) private var access
    @Environment(\.dismiss) private var dismiss
    @State private var preview: LonghornAttachmentPreview?
    @State private var selected = ""
    @State private var typed = ""
    @State private var error: String?
    init(target: LonghornAttachmentTarget, onConfirm: @escaping (String) -> Void) { self.target = target; self.onConfirm = onConfirm }
    var body: some View {
        let denial = access?.denial(for: .longhornAttachment, in: target.volume.namespace)
        NavigationStack {
            Form {
                Section {
                    Text("Changing a volume attachment may interrupt its workload. Detach uses the ordinary Longhorn API attachment; a workload controller may keep or restore its own attachment.").foregroundStyle(.statusWarn)
                }
                if preview == nil && error == nil { ProgressView() }
                if let error { ErrorLine(error: error) }
                if let p = preview {
                    Section {
                        if !p.error.isEmpty { ErrorLine(error: p.error) }
                        if !(target.attach ? p.canAttach : p.canDetach) { Text("This volume cannot change attachment in its current state.") }
                        if target.attach {
                            Text("Choose an available node")
                            ForEach(p.nodes) { node in
                                Button { selected = node.name } label: {
                                    HStack { Text(verbatim: node.name); Spacer(); if selected == node.name { Image(systemName: "checkmark") } }
                                }.disabled(!node.eligible)
                            }
                        } else { Text("Currently attached to: \(p.node)") }
                    }
                }
                Section(typedConfirmationPrompt(target.volume.name)) {
                    TextField("Volume name", text: $typed).autocorrectionDisabled().textInputAutocapitalization(.never)
                    Button { onConfirm(selected) } label: { if target.attach { Text("Attach volume") } else { Text("Detach volume") } }
                        .disabled(preview?.accepts(selected, attach: target.attach) != true || !typedConfirmationMatches(typed, token: target.volume.name) || denial != nil)
                    KubeDeniedNote(.longhornAttachment, in: target.volume.namespace)
                }
            }
            .navigationTitle(target.attach ? String(localized: "Attach volume") : String(localized: "Detach volume"))
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } } }
        }
        .task(id: target.id) {
            guard let client = model.client else { error = String(localized: "Not connected"); return }
            do { preview = try await client.longhornAttachmentPreview(namespace: target.volume.namespace, name: target.volume.name); if !target.attach { selected = preview?.node ?? "" } }
            catch { self.error = error.localizedDescription }
        }
    }
}
