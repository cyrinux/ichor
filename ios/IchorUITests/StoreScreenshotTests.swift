import XCTest

/// The App Store's iPhone screenshots in the app's ten languages, from the demo on a 6.9"
/// simulator (scripts/ios-build.sh store-screenshots, then scripts/ios-store-screenshots.sh): the
/// twin of Android's StoreScreenshotTest. Skipped unless the run asks for it, so the smoke and
/// website runs stay the same. One test per language, so that a stalled simulator retries one
/// language, not the whole set. Each screen is an attachment named FOLDER_N_NAME, FOLDER being the
/// App Store Connect language folder and N_NAME the listing order (ListingWalk.shots).
final class StoreScreenshotTests: XCTestCase {
    override func setUpWithError() throws {
        continueAfterFailure = false
        try XCTSkipUnless(
            ProcessInfo.processInfo.environment["ICHOR_STORE_SHOTS"] == "1",
            "Only for the App Store screenshots run (TEST_RUNNER_ICHOR_STORE_SHOTS=1)")
    }

    @MainActor func testEnglish() { walk("en-US", language: "en", locale: "en_US") }
    @MainActor func testFrench() { walk("fr-FR", language: "fr", locale: "fr_FR") }
    @MainActor func testSpanish() { walk("es-ES", language: "es", locale: "es_ES") }
    @MainActor func testUkrainian() { walk("uk", language: "uk", locale: "uk_UA") }
    @MainActor func testGerman() { walk("de-DE", language: "de", locale: "de_DE") }
    @MainActor func testItalian() { walk("it-IT", language: "it", locale: "it_IT") }
    @MainActor func testChinese() { walk("zh-Hans", language: "zh-Hans", locale: "zh_CN") }
    @MainActor func testJapanese() { walk("ja", language: "ja", locale: "ja_JP") }
    @MainActor func testPortuguese() { walk("pt-BR", language: "pt-BR", locale: "pt_BR") }
    @MainActor func testRussian() { walk("ru", language: "ru", locale: "ru_RU") }

    /// The seven listing screens with the app in `language`: the launch arguments set the app's
    /// language and region for that launch only, as Xcode's scheme options do.
    @MainActor
    private func walk(_ folder: String, language: String, locale: String) {
        let arguments = ["-AppleLanguages", "(\(language))", "-AppleLocale", locale]
        captureListingScreens(arguments: arguments) { subject in
            "\(folder)_\(ListingWalk.number(of: subject))_\(subject)"
        }
    }
}
