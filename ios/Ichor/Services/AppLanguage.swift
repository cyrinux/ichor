import Foundation

/// The language the app is shown in ("fr"): the AI answers in it.
var appLanguage: String { Bundle.main.preferredLocalizations.first ?? "en" }
