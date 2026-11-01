package name.levis.ichor

import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.assert
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.assertIsOff
import androidx.compose.ui.test.assertIsOn
import androidx.compose.ui.test.hasContentDescription
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.isToggleable
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.ImportConflict
import name.levis.ichor.model.kubeProblemText
import name.levis.ichor.ui.importconfig.ImportState
import name.levis.ichor.ui.importconfig.KubePreviewCard
import name.levis.ichor.ui.theme.TalosTheme
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

/** What TalkBack gets from a kubeconfig preview: one labeled target per choice. */
@RunWith(AndroidJUnit4::class)
class KubeImportPreviewSemanticsTest {
    @get:Rule
    val compose = createComposeRule()

    private val context get() = InstrumentationRegistry.getInstrumentation().targetContext
    private val problem = "kube-file-path"
    private val included = mutableListOf<Pair<Int, Boolean>>()
    private val replaced = mutableListOf<Pair<Int, Boolean>>()

    private fun include(name: String) = context.getString(R.string.import_include_context, name)

    private fun show() {
        val summary = ConfigSummary(
            contexts = listOf(
                ContextSummary(name = "dev"),
                ContextSummary(name = "staging"),
                ContextSummary(name = "local", problem = problem),
            ),
            current = "dev",
        )
        val preview = ImportState.KubePreview(
            yaml = "",
            summary = summary,
            conflicts = listOf(ImportConflict(1, suggested = "staging-2", sameAs = "staging")),
        )
        compose.setContent {
            TalosTheme {
                KubePreviewCard(
                    preview = preview,
                    adding = false,
                    onInclude = { index, on -> included += index to on },
                    onRename = { _, _ -> },
                    onReplace = { index, on -> replaced += index to on },
                    onConfirm = {},
                    onCancel = {},
                )
            }
        }
    }

    @Test
    fun oneTargetPerChoice() {
        show()
        // Three contexts and one replace row; the checkboxes and the switch inside add none.
        compose.onAllNodes(isToggleable()).assertCountEquals(4)
    }

    @Test
    fun aContextIsNamedAndTogglesFromItsRow() {
        show()
        val current = context.getString(R.string.import_context_current, "dev")
        compose.onNode(hasContentDescription(include(current)) and isToggleable())
            .assertIsOn()
            .assertIsEnabled()
            .performScrollTo()
            .performClick()
        compose.runOnIdle { assertEquals(listOf(0 to false), included) }
        compose.onNode(hasContentDescription(include("staging")) and isToggleable()).assertIsOn()
    }

    @Test
    fun theReplaceSwitchSaysWhichStoredClusterItChanges() {
        show()
        val label = context.getString(R.string.import_name_replace_a11y, "staging")
        compose.onNode(hasContentDescription(label) and isToggleable())
            .assertIsOff()
            .performScrollTo()
            .performClick()
        compose.runOnIdle { assertEquals(listOf(1 to true), replaced) }
    }

    @Test
    fun aContextThatCannotBeAddedSaysWhyOnce() {
        show()
        val reason = context.getString(kubeProblemText(problem))
        compose.onNode(hasContentDescription(include("local")) and isToggleable())
            .assertIsOff()
            .assertIsNotEnabled()
            .assert(SemanticsMatcher.expectValue(SemanticsProperties.StateDescription, reason))
        // Drawn for sighted users, not a second stop that repeats it.
        compose.onAllNodes(hasText(reason)).assertCountEquals(0)
    }
}
