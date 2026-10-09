/// What the build may show, by distribution channel. The App Store build (`scripts/ios-build.sh
/// appstore`, compiled with APP_STORE) has no donation links: Apple only lets approved
/// nonprofits take donations outside in-app purchase (App Review Guideline 3.2.2), as the Play
/// build on Android.
/// `appStore`: Apple delivers its updates (App Store or TestFlight); the other builds are
/// sideloaded and reinstalled by hand.
enum Distribution {
    #if APP_STORE
    static let appStore = true
    static let donations = false
    #else
    static let appStore = false
    static let donations = true
    #endif
}
