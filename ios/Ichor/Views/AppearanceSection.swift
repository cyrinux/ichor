import SwiftUI
import IchorCore

/// Settings → Appearance: theme and language (live cluster usage is its own section).
struct AppearanceSection: View {
    @Environment(AppModel.self) private var model
    @Environment(\.openURL) private var openURL

    var body: some View {
        @Bindable var model = model
        Section {
            Picker("Theme", selection: $model.theme) {
                ForEach(ThemeMode.allCases) { Text($0.label).tag($0) }
            }
            .pickerStyle(.segmented)
            // iOS manages the per-app language in the Settings app (the bundle declares its localizations).
            Button {
                if let url = URL(string: UIApplication.openSettingsURLString) { openURL(url) }
            } label: {
                LabeledContent("Language", value: Self.currentLanguage)
            }
            .foregroundStyle(.primary)
        } header: {
            Text("Appearance")
        } footer: {
            Text(
                "Ichor follows the iPhone language. To use another one for this app only, open its page in the Settings app and choose Language."
            )
        }
    }

    /// The language the app is shown in, named in that language ("Français", "Deutsch"…).
    private static var currentLanguage: String {
        let code = Bundle.main.preferredLocalizations.first ?? "en"
        let name = Locale(identifier: code).localizedString(forLanguageCode: code) ?? code
        return name.prefix(1).uppercased() + name.dropFirst()
    }
}
