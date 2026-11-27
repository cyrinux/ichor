package name.levis.ichor.baselineprofile

import android.content.Intent
import android.net.Uri
import androidx.benchmark.macro.junit4.BaselineProfileRule
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.filters.LargeTest
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.uiautomator.By
import androidx.test.uiautomator.BySelector
import androidx.test.uiautomator.Direction
import androidx.test.uiautomator.UiDevice
import androidx.test.uiautomator.UiObject2
import androidx.test.uiautomator.Until
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

/**
 * The paths a person takes in the first seconds after an install or an update, recorded into
 * app/src/main/generated/baselineProfiles/baseline-prof.txt: a cold start into the offline demo
 * (`ichor://demo`), its overview, a node, the node's kernel log, and back, each list scrolled
 * once. Run with `just baseline-profile` on an API 33+ emulator (or a rooted one); the release
 * builds then compile these paths at install time.
 *
 * The anchors are the smoke test's tags (app ui/components/SmokeTags.kt), which the app exposes
 * as resource ids to UI Automator (MainActivity, testTagsAsResourceId).
 */
@LargeTest
@RunWith(AndroidJUnit4::class)
class BaselineProfileGenerator {
    @get:Rule
    val rule = BaselineProfileRule()

    @Test
    fun startupAndTheDemoWalk() {
        // Set in build.gradle.kts from the tested APK: the release id, with its .foss suffix.
        val target = InstrumentationRegistry.getArguments().getString("targetAppId")
            ?: error("targetAppId is not set: run through :app:generateBaselineProfile")
        rule.collect(packageName = target) {
            pressHome()
            startActivityAndWait(Intent(Intent.ACTION_VIEW, Uri.parse("ichor://demo")).setPackage(packageName))
            device.skipTour()

            device.waitForObject(By.scrollable(true)).fling(Direction.DOWN)
            device.waitForObject(By.scrollable(true)).fling(Direction.UP)
            device.waitForObject(By.res(NODE)).click()

            val nodeScreen = device.waitForObject(By.res(NODE_SCREEN))
            nodeScreen.scrollUntil(Direction.DOWN, Until.findObject(By.res(KERNEL_LOG)))
            device.waitForObject(By.res(KERNEL_LOG)).click()

            device.waitForObject(By.res(LOG_LINES)).fling(Direction.UP)
            device.pressBack()
            device.waitForObject(By.res(NODE_SCREEN))
            device.pressBack()
            device.waitForObject(By.res(NODE))
        }
    }

    /** The demo tour shows on the first open only; later iterations go straight to the overview. */
    private fun UiDevice.skipTour() {
        wait(Until.findObject(By.res(TOUR_SKIP)), TOUR_TIMEOUT_MS)?.click()
    }

    private fun UiDevice.waitForObject(selector: BySelector): UiObject2 =
        wait(Until.findObject(selector), TIMEOUT_MS)
            ?: throw AssertionError("$selector did not show within $TIMEOUT_MS ms")

    private companion object {
        const val TIMEOUT_MS = 30_000L
        const val TOUR_TIMEOUT_MS = 5_000L

        // The app's SmokeTags.kt (another APK: its constants are not on this classpath).
        const val TOUR_SKIP = "smoke-tour-skip"
        const val NODE = "smoke-node"
        const val NODE_SCREEN = "smoke-node-screen"
        const val KERNEL_LOG = "smoke-node-menu-KERNEL_LOG"
        const val LOG_LINES = "smoke-log-lines"
    }
}
