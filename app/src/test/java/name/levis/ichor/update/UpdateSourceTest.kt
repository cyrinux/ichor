package name.levis.ichor.update

import org.junit.Assert.assertEquals
import org.junit.Test

class UpdateSourceTest {

    @Test
    fun playBuildUpdatesFromGooglePlay() {
        assertEquals(UpdateSource.PLAY, updateSource(buildType = "play", selfUpdate = false))
    }

    @Test
    fun playWinsEvenIfSelfUpdateWereOn() {
        assertEquals(UpdateSource.PLAY, updateSource(buildType = "play", selfUpdate = true))
    }

    @Test
    fun openSourceBuildsUpdateThemselves() {
        assertEquals(UpdateSource.SELF, updateSource(buildType = "release", selfUpdate = true))
        assertEquals(UpdateSource.SELF, updateSource(buildType = "debug", selfUpdate = true))
    }

    @Test
    fun otherBuildsLeaveUpdatesToTheirInstaller() {
        assertEquals(UpdateSource.INSTALLER, updateSource(buildType = "release", selfUpdate = false))
    }
}
