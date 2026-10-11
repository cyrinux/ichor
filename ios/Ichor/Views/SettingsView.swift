import SwiftUI
import IchorCore

/// Settings: one row per group (see IchorCore's SettingsPage), each opening its own page.
struct SettingsView: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        Form {
            ForEach(SettingsPage.visible(kubeconfig: model.allows(.kubeconfig)), id: \.self) { page in
                NavigationLink(value: Route.settingsPage(page)) {
                    Label {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(page.title)
                            Text(page.summary).font(.caption).foregroundStyle(.secondary)
                        }
                    } icon: {
                        Image(systemName: page.systemImage)
                    }
                }
            }
        }
        .themedBackground()
        .navigationTitle("Settings")
    }
}
