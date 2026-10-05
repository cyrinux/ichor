/// What the build may show, by distribution channel. The App Store build (`scripts/ios-build.sh
/// appstore`, compiled with APP_STORE) has no donation links: Apple only lets approved
/// nonprofits take donations outside in-app purchase (App Review Guideline 3.2.2), as the Play
/// build on Android.
enum Distribution {
    #if APP_STORE
    static let donations = false
    #else
    static let donations = true
    #endif
}
