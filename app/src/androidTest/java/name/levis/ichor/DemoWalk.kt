package name.levis.ichor

import android.content.Intent
import android.net.Uri
import android.os.ParcelFileDescriptor
import androidx.compose.ui.test.ComposeTimeoutException
import androidx.compose.ui.test.ExperimentalTestApi
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.hasScrollToNodeAction
import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.test.isEnabled
import androidx.compose.ui.test.junit4.ComposeTestRule
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.test.performScrollToNode
import androidx.compose.ui.test.printToString
import androidx.test.core.app.ActivityScenario
import androidx.test.core.app.ApplicationProvider
import androidx.test.platform.app.InstrumentationRegistry
import name.levis.ichor.ui.components.SMOKE_CAPTURE
import name.levis.ichor.ui.components.SMOKE_CAPTURE_START
import name.levis.ichor.ui.components.SMOKE_CLUSTER_TITLE
import name.levis.ichor.ui.components.SMOKE_CLUSTER_UPGRADE
import name.levis.ichor.ui.components.SMOKE_EVENTS
import name.levis.ichor.ui.components.SMOKE_GRAPHS
import name.levis.ichor.ui.components.SMOKE_LOG_LINES
import name.levis.ichor.ui.components.SMOKE_NODE
import name.levis.ichor.ui.components.SMOKE_NODE_SCREEN
import name.levis.ichor.ui.components.SMOKE_NODE_SERVICES
import name.levis.ichor.ui.components.SMOKE_TOUR_SKIP
import name.levis.ichor.ui.components.SMOKE_UPGRADE
import name.levis.ichor.ui.components.smokeMenuTag
import name.levis.ichor.ui.node.NodeMenuEntry

/**
 * Walks the offline demo and captures its screens as raw screencaps in [dir], for DemoScreenshots
 * and StoreScreenshotTest. Every screen is first waited for by a tag on its loaded content; the
 * pause after it is for what has to build up on screen (the graphs, the packets).
 */
@OptIn(ExperimentalTestApi::class)
class DemoWalk(private val compose: ComposeTestRule, private val dir: String) {
    init {
        shell("mkdir -p $dir")
    }

    /** The seven screens of the store listing; the names match docs/screenshots/. */
    fun captureListingScreens() {
        openDemo().use {
            capture("overview")
        }
        NODE_SHOTS.forEach { shot ->
            openDemo().use {
                openNodeEntry(shot.entry)
                waitFor(hasTestTag(shot.loaded))
                capture(shot.name, if (shot.entry == NodeMenuEntry.TAB_LIVE) GRAPHS_SETTLE else SETTLE)
            }
        }
        openDemo().use {
            openNodeEntry(NodeMenuEntry.CAPTURE)
            val start = hasTestTag(SMOKE_CAPTURE_START) and isEnabled()
            waitFor(start)
            compose.onNode(start).performScrollTo().performClick()
            waitFor(hasTestTag(SMOKE_CAPTURE))
            capture("capture", CAPTURE_SETTLE)
        }
        openDemo().use {
            waitFor(hasTestTag(SMOKE_CLUSTER_TITLE))
            compose.onNode(hasTestTag(SMOKE_CLUSTER_TITLE)).performClick()
            waitFor(hasTestTag(SMOKE_CLUSTER_UPGRADE))
            compose.onNode(hasTestTag(SMOKE_CLUSTER_UPGRADE)).performClick()
            waitFor(hasTestTag(SMOKE_UPGRADE))
            capture("upgrade", UPGRADE_SETTLE)
        }
    }

    fun open(link: String): ActivityScenario<MainActivity> {
        // The images show the app as after its demo notice is closed.
        ApplicationProvider.getApplicationContext<TalosApp>().uiPreferences.dismissDemoNotice()
        return ActivityScenario.launch(
            Intent(Intent.ACTION_VIEW, Uri.parse(link)).setClass(ApplicationProvider.getApplicationContext(), MainActivity::class.java),
        )
    }

    /** The Talos demo's overview, past the tour a fresh install shows once. */
    fun openDemo(): ActivityScenario<MainActivity> {
        val scenario = open("ichor://demo")
        val skip = hasTestTag(SMOKE_TOUR_SKIP)
        // The tour shows on the first open only; then the overview's list, its nodes maybe
        // below the fold (a lazy list composes only what shows).
        waitFor(skip or hasScrollToNodeAction())
        compose.waitForIdle()
        if (exists(skip)) {
            compose.onNode(skip).performClick()
            compose.waitUntilDoesNotExist(skip, TIMEOUT)
        }
        return scenario
    }

    /** Lets the screen load its demo data, then takes the raw screen as [name].png. */
    fun capture(name: String, settle: Long = SETTLE) {
        compose.waitForIdle()
        settle(settle)
        compose.waitForIdle()
        shell("screencap -p $dir/$name.png")
    }

    /** Waits for [matcher]; on a timeout, failure.png and the semantics tree say what showed. */
    fun waitFor(matcher: SemanticsMatcher) {
        try {
            compose.waitUntilAtLeastOneExists(matcher, TIMEOUT)
        } catch (e: ComposeTimeoutException) {
            shell("screencap -p $dir/failure.png")
            val tree = runCatching { compose.onRoot(useUnmergedTree = true).printToString() }.getOrElse { "no Compose root: $it" }
            throw AssertionError("${e.message}\non screen:\n$tree", e)
        }
    }

    private fun openNodeEntry(entry: NodeMenuEntry) {
        scrollTo(hasTestTag(SMOKE_NODE))
        compose.onAllNodes(hasTestTag(SMOKE_NODE))[0].performClick()
        val tag = hasTestTag(smokeMenuTag(entry))
        scrollTo(tag, within = hasTestTag(SMOKE_NODE_SCREEN))
        compose.onNode(tag).performClick()
    }

    /**
     * Lets [millis] pass in real time and on the test's clock: the screens' polling delays run
     * on that clock, the data they sample (the graphs' rates, a capture) on the real one.
     */
    private fun settle(millis: Long) {
        repeat((millis / SETTLE_STEP).toInt()) {
            Thread.sleep(SETTLE_STEP)
            compose.mainClock.advanceTimeBy(SETTLE_STEP)
        }
    }

    private fun exists(matcher: SemanticsMatcher) = compose.onAllNodes(matcher).fetchSemanticsNodes().isNotEmpty()

    private fun shell(command: String) {
        val output = InstrumentationRegistry.getInstrumentation().uiAutomation.executeShellCommand(command)
        ParcelFileDescriptor.AutoCloseInputStream(output).use { it.readBytes() }
    }

    private fun scrollTo(matcher: SemanticsMatcher, within: SemanticsMatcher = hasScrollToNodeAction()) {
        waitFor(within)
        compose.onAllNodes(within)[0].performScrollToNode(matcher)
        waitFor(matcher)
    }

    /** A screen opened from a node's menu, and the tag on its loaded content. */
    private data class NodeShot(val name: String, val entry: NodeMenuEntry, val loaded: String)

    private companion object {
        const val TIMEOUT = 30_000L
        const val SETTLE = 2_500L

        /** Most of the live graphs' three minutes, so that they draw more than a sliver. */
        const val GRAPHS_SETTLE = 150_000L

        /** A screenful of sample packets. */
        const val CAPTURE_SETTLE = 8_000L

        /** The releases (from the network), then the plan of the newest. */
        const val UPGRADE_SETTLE = 8_000L
        const val SETTLE_STEP = 500L

        /** The gallery order after the overview; names match docs/screenshots/. */
        val NODE_SHOTS = listOf(
            NodeShot("node-services", NodeMenuEntry.TAB_SERVICES, SMOKE_NODE_SERVICES),
            NodeShot("logs", NodeMenuEntry.KERNEL_LOG, SMOKE_LOG_LINES),
            NodeShot("events", NodeMenuEntry.EVENTS, SMOKE_EVENTS),
            NodeShot("graphs", NodeMenuEntry.TAB_LIVE, SMOKE_GRAPHS),
        )
    }
}
