package name.levis.ichor.model

import name.levis.ichor.R
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class KubeImportTest {

    // As ParseKubeconfig lists them: sorted by name, one that cannot be added.
    private val summary = ConfigSummary(
        current = "prod",
        contexts = listOf(
            ContextSummary("eks", kind = KIND_KUBE, auth = "eks", problem = "kube-sign-in-later"),
            ContextSummary("lab", kind = KIND_KUBE, auth = "token"),
            ContextSummary("prod", kind = KIND_KUBE, auth = "cert"),
        ),
    )

    @Test
    fun importableContextsStartChecked() {
        assertEquals(
            listOf(ImportChoice(0, skip = true), ImportChoice(1), ImportChoice(2)),
            initialKubeChoices(summary),
        )
    }

    @Test
    fun rowsFollowTheChoices() {
        val conflicts = listOf(ImportConflict(1, "lab-1", sameAs = "lab"))
        val rows = kubeImportRows(summary, conflicts, initialKubeChoices(summary))

        assertEquals(listOf(false, true, true), rows.map { it.included })
        assertFalse(rows[0].importable)
        assertEquals(conflicts[0], rows[1].conflict)
        assertNull(rows[2].conflict)
    }

    @Test
    fun anUncheckedContextClashesWithNothing() {
        val conflicts = listOf(ImportConflict(1, "lab-1"))
        val choices = initialKubeChoices(summary).map { if (it.index == 1) it.copy(skip = true) else it }

        val rows = kubeImportRows(summary, conflicts, choices)

        assertFalse(rows[1].included)
        assertNull(rows[1].conflict)
    }

    @Test
    fun aContextWithAProblemStaysOutWhateverTheChoice() {
        val rows = kubeImportRows(summary, emptyList(), listOf(ImportChoice(0)))

        assertFalse(rows[0].included)
        // Missing choices default like initialKubeChoices.
        assertTrue(rows[1].included)
    }

    @Test
    fun skippedChoicesTakeNoName() {
        val conflicts = listOf(ImportConflict(0, "lab-1"), ImportConflict(1, "lab-2"))
        // The skipped one typed a name the other picks: no clash, it is not imported.
        val choices = listOf(ImportChoice(0, name = "home", skip = true), ImportChoice(1, name = "home"))

        assertEquals(emptySet<Int>(), takenNameChoices(conflicts, choices, setOf("lab"), listOf("lab", "lab")))
    }

    @Test
    fun labelsForKnownCodes() {
        assertEquals(R.string.kube_auth_cert, kubeAuthLabel("cert"))
        assertEquals(R.string.kube_auth_provider, kubeAuthLabel("auth-provider"))
        assertNull(kubeAuthLabel("something-new"))
        assertEquals(R.string.kube_problem_file_path, kubeProblemText("kube-file-path"))
        assertEquals(R.string.kube_problem_sign_in_later, kubeProblemText("kube-sign-in-later"))
        // A code from a newer core still says something.
        assertEquals(R.string.kube_problem_invalid, kubeProblemText("kube-something-new"))
    }
}
