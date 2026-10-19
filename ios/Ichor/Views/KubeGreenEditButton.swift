import SwiftUI
import IchorCore

/// "Edit schedule", off with the reason when the credentials may not patch the SleepInfo. Under
/// the kube-green plugin's SleepInfo rows (PluginList).
struct KubeGreenEditButton: View {
    let info: KubeGreenSleepInfo
    let onEdit: () -> Void
    @Environment(AppModel.self) private var model
    @State private var denial: KubeAccess?

    var body: some View {
        Button("Edit schedule", action: onEdit)
            .disabled(denial != nil)
            .task(id: info.id) {
                guard let client = model.client,
                      let access = try? await client.kubeCan(verb: "patch", group: "kube-green.com", resource: "sleepinfos",
                                                             namespace: info.namespace, name: info.name) else { return }
                denial = access.isDenied ? access : nil
            }
        if let denial { KubeDeniedLine(text: denial.deniedText) }
    }
}
