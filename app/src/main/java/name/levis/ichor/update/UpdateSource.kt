package name.levis.ichor.update

import name.levis.ichor.BuildConfig

/** Where this build's updates come from, as Settings → Updates says. */
enum class UpdateSource {
    /** The Play build: Google Play delivers updates (in-app via [StoreUpdater]). */
    PLAY,

    /** The open-source builds with the self-updater: GitHub releases, through [UpdateManager]. */
    SELF,

    /** A build without either: whatever installed it (a store, Obtainium…) updates it. */
    INSTALLER,
}

/** The Play build type's name in app/build.gradle.kts. */
private const val PLAY_BUILD_TYPE = "play"

/** Defaults to this build; the parameters exist for tests. */
fun updateSource(
    buildType: String = BuildConfig.BUILD_TYPE,
    selfUpdate: Boolean = BuildConfig.SELF_UPDATE,
): UpdateSource = when {
    buildType == PLAY_BUILD_TYPE -> UpdateSource.PLAY
    selfUpdate -> UpdateSource.SELF
    else -> UpdateSource.INSTALLER
}
