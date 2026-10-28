package name.levis.ichor

import android.content.Intent
import android.net.Uri
import androidx.compose.ui.test.ExperimentalTestApi
import androidx.compose.ui.test.hasScrollToNodeAction
import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.test.junit4.createEmptyComposeRule
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToNode
import androidx.test.core.app.ActivityScenario
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import name.levis.ichor.ui.components.SMOKE_KUBE_HOME
import name.levis.ichor.ui.components.SMOKE_LOG_LINES
import name.levis.ichor.ui.components.SMOKE_NODE
import name.levis.ichor.ui.components.smokeMenuTag
import name.levis.ichor.ui.node.NodeMenuEntry
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

/**
 * The demo cluster walked on an emulator (CYR-472): the crash-on-launch and blank-screen
 * regressions unit tests cannot see. The demo is offline (`ichor://demo`, `ichor://demo/kube`);
 * every step waits for a test tag (ui/components/SmokeTags.kt), never for a fixed time.
 */
@OptIn(ExperimentalTestApi::class)
@RunWith(AndroidJUnit4::class)
class DemoSmokeTest {
    @get:Rule
    val compose = createEmptyComposeRule()

    private fun open(link: String): ActivityScenario<MainActivity> =
        ActivityScenario.launch(
            Intent(Intent.ACTION_VIEW, Uri.parse(link)).setClass(ApplicationProvider.getApplicationContext(), MainActivity::class.java),
        )

    @Test
    fun theTalosDemoOpensANodeAndItsKernelLog() {
        open("ichor://demo").use {
            compose.waitUntilAtLeastOneExists(hasTestTag(SMOKE_NODE), TIMEOUT)
            compose.onAllNodes(hasTestTag(SMOKE_NODE))[0].performClick()

            val kernelLog = hasTestTag(smokeMenuTag(NodeMenuEntry.KERNEL_LOG))
            compose.waitUntilAtLeastOneExists(hasScrollToNodeAction(), TIMEOUT)
            compose.onAllNodes(hasScrollToNodeAction())[0].performScrollToNode(kernelLog)
            compose.onNode(kernelLog).performClick()

            compose.waitUntilAtLeastOneExists(hasTestTag(SMOKE_LOG_LINES), TIMEOUT)
        }
    }

    @Test
    fun theKubernetesDemoOpensItsHome() {
        open("ichor://demo/kube").use {
            compose.waitUntilAtLeastOneExists(hasTestTag(SMOKE_KUBE_HOME), TIMEOUT)
        }
    }

    private companion object {
        /** A cold emulator: the first screen of a fresh install can take a while. */
        const val TIMEOUT = 30_000L
    }
}
