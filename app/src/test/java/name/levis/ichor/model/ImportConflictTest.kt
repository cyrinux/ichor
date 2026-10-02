package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Test

class ImportConflictTest {

    // Imported "lab" and "staging" both clash with stored clusters.
    private val conflicts = listOf(ImportConflict(0, "lab-1", sameAs = "lab"), ImportConflict(1, "staging-1"))
    private val stored = setOf("lab", "prod", "staging")
    private val imported = listOf("lab", "staging")

    private fun taken(vararg choices: ImportChoice) = takenNameChoices(conflicts, choices.toList(), stored, imported)

    @Test
    fun defaultsAndFreeNamesAreFine() {
        assertEquals(emptySet<Int>(), taken(ImportChoice(0), ImportChoice(1)))
        assertEquals(emptySet<Int>(), taken(ImportChoice(0, name = " home lab "), ImportChoice(1, name = "stage")))
        // Its own suggestion, typed out.
        assertEquals(emptySet<Int>(), taken(ImportChoice(0, name = "lab-1"), ImportChoice(1)))
    }

    @Test
    fun storedAndImportedNamesAreTaken() {
        assertEquals(setOf(0), taken(ImportChoice(0, name = "prod "), ImportChoice(1)))
        assertEquals(setOf(1), taken(ImportChoice(0), ImportChoice(1, name = "lab")))
    }

    @Test
    fun choicesCannotShareANameOrTakeAnotherSuggestion() {
        assertEquals(setOf(0, 1), taken(ImportChoice(0, name = "dev"), ImportChoice(1, name = "dev")))
        assertEquals(setOf(1), taken(ImportChoice(0), ImportChoice(1, name = "lab-1")))
    }

    @Test
    fun replacingIgnoresTheTypedName() {
        assertEquals(emptySet<Int>(), taken(ImportChoice(0, name = "prod", replace = true), ImportChoice(1)))
    }
}
