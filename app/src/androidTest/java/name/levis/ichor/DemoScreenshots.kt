package name.levis.ichor

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.ParcelFileDescriptor
import androidx.compose.ui.test.ComposeTimeoutException
import androidx.compose.ui.test.ExperimentalTestApi
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.hasScrollToNodeAction
import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.isEnabled
import androidx.compose.ui.test.junit4.createEmptyComposeRule
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.test.performScrollToNode
import androidx.compose.ui.test.printToString
import androidx.test.core.app.ActivityScenario
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import name.levis.ichor.ui.components.SMOKE_CAPTURE_START
import name.levis.ichor.ui.components.SMOKE_CLUSTER_TITLE
import name.levis.ichor.ui.components.SMOKE_CLUSTER_UPGRADE
import name.levis.ichor.ui.components.SMOKE_KUBE_HOME
import name.levis.ichor.ui.components.SMOKE_NODE
import name.levis.ichor.ui.components.SMOKE_NODE_SCREEN
import name.levis.ichor.ui.components.SMOKE_TOUR_SKIP
import name.levis.ichor.ui.components.smokeMenuTag
import name.levis.ichor.model.DataServiceKind
import name.levis.ichor.model.KubeFocus
import name.levis.ichor.ui.nav.Routes
import name.levis.ichor.ui.nav.TestRoute
import name.levis.ichor.ui.node.CGROUPS_TAB
import name.levis.ichor.ui.node.NodeMenuEntry
import name.levis.ichor.ui.settings.SettingsPage
import name.levis.ichor.ui.workloads.NETPERF_TAB
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
            capture("overview")
        }
        NODE_SHOTS.forEach { (name, entry) ->
            openDemo().use {
                openNodeEntry(entry)
                capture(name, if (entry == NodeMenuEntry.TAB_LIVE) GRAPHS_SETTLE else SETTLE)
            }
        }
        openDemo().use {
            openNodeEntry(NodeMenuEntry.CAPTURE)
            val start = hasTestTag(SMOKE_CAPTURE_START) and isEnabled()
            waitFor(start)
            compose.onNode(start).performScrollTo().performClick()
            capture("capture", CAPTURE_SETTLE)
        }
        openDemo().use {
            waitFor(hasTestTag(SMOKE_CLUSTER_TITLE))
            compose.onNode(hasTestTag(SMOKE_CLUSTER_TITLE)).performClick()
            waitFor(hasTestTag(SMOKE_CLUSTER_UPGRADE))
            compose.onNode(hasTestTag(SMOKE_CLUSTER_UPGRADE)).performClick()
            capture("upgrade", UPGRADE_SETTLE)
        }
    }

    /** The screens of the website's feature cards, each opened directly from the demo. */
    @Test
    fun captureTheFeatures() {
        shell("mkdir -p $DIR")
        FEATURE_SHOTS.forEach { (name, route) ->
            openDemo().use {
                TestRoute.route.value = route
                capture("feature-$name", FEATURE_SETTLE)
            }
        }
        // The recorder is the Insights screen's second tab.
        openDemo().use {
            TestRoute.route.value = Routes.INSIGHTS
            val recorder = hasText(ApplicationProvider.getApplicationContext<Context>().getString(R.string.insights_tab_recorder))
            waitFor(recorder)
            compose.onNode(recorder).performClick()
            capture("feature-recorder", FEATURE_SETTLE)
        }
        open("ichor://demo/kube").use {
            waitFor(hasTestTag(SMOKE_KUBE_HOME))
            capture("feature-kube-home", FEATURE_SETTLE)
        }
    }

    /** The Talos demo's overview, past the tour a fresh install shows once. */
    private fun open(link: String): ActivityScenario<MainActivity> =
        ActivityScenario.launch(
            Intent(Intent.ACTION_VIEW, Uri.parse(link)).setClass(ApplicationProvider.getApplicationContext(), MainActivity::class.java),
        )

    private fun openDemo(): ActivityScenario<MainActivity> {
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
        settle(settle)
        compose.waitForIdle()
        shell("screencap -p $DIR/$name.png")
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

    /** Waits for [matcher]; on a timeout, failure.png and the semantics tree say what showed. */
    private fun waitFor(matcher: SemanticsMatcher) {
        try {
            compose.waitUntilAtLeastOneExists(matcher, TIMEOUT)
        } catch (e: ComposeTimeoutException) {
            shell("screencap -p $DIR/failure.png")
            val tree = runCatching { compose.onRoot(useUnmergedTree = true).printToString() }.getOrElse { "no Compose root: $it" }
            throw AssertionError("${e.message}\non screen:\n$tree", e)
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

        /** Most of the live graphs' three minutes, so that they draw more than a sliver. */
        const val GRAPHS_SETTLE = 150_000L

        /** A screenful of sample packets. */
        const val CAPTURE_SETTLE = 8_000L

        /** The releases (from the network), then the plan of the newest. */
        const val UPGRADE_SETTLE = 8_000L
        const val SETTLE_STEP = 500L
        const val FEATURE_SETTLE = 5_000L

        /** Names match scripts/ci-screenshots.sh and docs/screenshots/feature-NAME.webp. */
        val FEATURE_SHOTS = listOf(
            "plugins" to Routes.settingsPage(SettingsPage.PLUGINS),
            "gitops" to Routes.gitOpsSource("argocd", "Application", "argocd", "grafana", 0),
            "capacity" to Routes.KUBE_CAPACITY,
            "runbooks" to Routes.RUNBOOKS,
            "activity" to Routes.activity(),
            "argocd-diff" to Routes.argoDiff("argocd", "grafana"),
            "argocd" to Routes.ARGO_CD,
            "metrics" to Routes.metrics(),
            "data-services" to Routes.dataServices(),
            "castai" to Routes.dataServices(DataServiceKind.CASTAI),
            "cgroups" to "node/tabs?addr=192.0.2.10&host=demo-cp-1&tab=$CGROUPS_TAB",
            "apps" to Routes.apps(),
            "workloads" to Routes.workloads(KubeFocus(0)),
            "pods" to Routes.workloads(KubeFocus(1)),
            "cronjobs" to Routes.workloads(KubeFocus(2)),
            "netperf" to Routes.workloads(KubeFocus(NETPERF_TAB)),
            "drift" to Routes.INSIGHTS,
            "import" to Routes.IMPORT,
        )

        /** The website's gallery order after the overview; names match docs/screenshots/. */
        val NODE_SHOTS = listOf(
            "node-services" to NodeMenuEntry.TAB_SERVICES,
            "logs" to NodeMenuEntry.KERNEL_LOG,
            "events" to NodeMenuEntry.EVENTS,
            "graphs" to NodeMenuEntry.TAB_LIVE,
        )
    }
}
