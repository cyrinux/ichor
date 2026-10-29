package name.levis.ichor

import android.content.Intent
import android.net.Uri
import android.os.ParcelFileDescriptor
import androidx.compose.ui.test.ComposeTimeoutException
import androidx.compose.ui.test.ExperimentalTestApi
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.hasScrollToNodeAction
import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.test.junit4.createEmptyComposeRule
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToNode
import androidx.compose.ui.test.printToString
import androidx.test.core.app.ActivityScenario
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import name.levis.ichor.ui.components.SMOKE_NODE
import name.levis.ichor.ui.components.SMOKE_NODE_SCREEN
import name.levis.ichor.ui.components.SMOKE_TOUR_SKIP
import name.levis.ichor.ui.components.smokeMenuTag
import name.levis.ichor.ui.node.NodeMenuEntry
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

/**
 * The website and store screenshots, taken from the offline demo on an emulator
 * (scripts/ci-screenshots.sh, workflow screenshots.yml). Not part of the smoke test: ui-smoke.sh
 * leaves this class out. Each screen is a raw screencap in [DIR]; the script crops the system
 * bars off and writes docs/screenshots/ and the Play Store copies.
 */
@OptIn(ExperimentalTestApi::class)
@RunWith(AndroidJUnit4::class)
class DemoScreenshots {
    @get:Rule
    val compose = createEmptyComposeRule()

    @Test
    fun captureTheDemo() {
        shell("mkdir -p $DIR")
        openDemo().use {
            scrollTo(hasTestTag(SMOKE_NODE))
            capture("overview")
        }
        NODE_SHOTS.forEach { (name, entry) ->
            openDemo().use {
                openNodeEntry(entry)
                capture(name, if (entry == NodeMenuEntry.TAB_LIVE) GRAPHS_SETTLE else SETTLE)
            }
        }
    }

    /** The Talos demo's overview, past the tour a fresh install shows once. */
    private fun openDemo(): ActivityScenario<MainActivity> {
        val scenario = ActivityScenario.launch<MainActivity>(
            Intent(Intent.ACTION_VIEW, Uri.parse("ichor://demo")).setClass(ApplicationProvider.getApplicationContext(), MainActivity::class.java),
        )
        val skip = hasTestTag(SMOKE_TOUR_SKIP)
        compose.waitUntil(TIMEOUT) { exists(skip) || exists(hasTestTag(SMOKE_NODE)) }
        if (exists(skip)) {
            compose.onNode(skip).performClick()
            compose.waitUntilDoesNotExist(skip, TIMEOUT)
        }
        return scenario
    }

    private fun openNodeEntry(entry: NodeMenuEntry) {
        scrollTo(hasTestTag(SMOKE_NODE))
        compose.onAllNodes(hasTestTag(SMOKE_NODE))[0].performClick()
        val tag = hasTestTag(smokeMenuTag(entry))
        scrollTo(tag, within = hasTestTag(SMOKE_NODE_SCREEN))
        compose.onNode(tag).performClick()
    }

    /** Lets the screen load its demo data, then takes the raw screen as [name].png. */
    private fun capture(name: String, settle: Long = SETTLE) {
        compose.waitForIdle()
        Thread.sleep(settle)
        compose.waitForIdle()
        shell("screencap -p $DIR/$name.png")
    }

    private fun exists(matcher: SemanticsMatcher) = compose.onAllNodes(matcher).fetchSemanticsNodes().isNotEmpty()

    private fun shell(command: String) {
        val output = InstrumentationRegistry.getInstrumentation().uiAutomation.executeShellCommand(command)
        ParcelFileDescriptor.AutoCloseInputStream(output).use { it.readBytes() }
    }

    private fun waitFor(matcher: SemanticsMatcher) {
        try {
            compose.waitUntilAtLeastOneExists(matcher, TIMEOUT)
        } catch (e: ComposeTimeoutException) {
            throw AssertionError("${e.message}\non screen:\n" + compose.onRoot(useUnmergedTree = true).printToString(), e)
        }
    }

    private fun scrollTo(matcher: SemanticsMatcher, within: SemanticsMatcher = hasScrollToNodeAction()) {
        waitFor(within)
        compose.onAllNodes(within)[0].performScrollToNode(matcher)
        waitFor(matcher)
    }

    private companion object {
        /** Where the raw captures go: the shell user writes here, ci-screenshots.sh pulls it. */
        const val DIR = "/data/local/tmp/ichor-screenshots"
        const val TIMEOUT = 30_000L
        const val SETTLE = 2_500L

        /** The live graphs need a few samples before they draw a line. */
        const val GRAPHS_SETTLE = 10_000L

        /** The website's gallery order after the overview; names match docs/screenshots/. */
        val NODE_SHOTS = listOf(
            "node-services" to NodeMenuEntry.TAB_SERVICES,
            "logs" to NodeMenuEntry.KERNEL_LOG,
            "events" to NodeMenuEntry.EVENTS,
            "graphs" to NodeMenuEntry.TAB_LIVE,
            "capture" to NodeMenuEntry.CAPTURE,
            "upgrade" to NodeMenuEntry.UPGRADE,
        )
    }
}
