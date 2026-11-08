package name.levis.ichor.ui.design

import java.io.File
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/** Screens draw the wrapped primitives through ui/design, so a new UI style changes that package only. */
class DesignLayerTest {
    private val direct = Regex(
        """^import androidx\.compose\.material3\.(\*|Card|OutlinedCard|TopAppBar|Scaffold|Button|TextButton|OutlinedButton|""" +
            """IconButton|FilterChip|AssistChip|Switch|AlertDialog|ListItem)$""",
        RegexOption.MULTILINE,
    )

    private fun moduleDir(): File {
        val dir = File(System.getProperty("user.dir")!!)
        return if (File(dir, "src/main/java").isDirectory) dir else File(dir, "app")
    }

    @Test
    fun `screens import no wrapped Material primitive`() {
        val module = moduleDir()
        val ui = File(module, "src/main/java/name/levis/ichor/ui")
        val allowed = File(module, "src/test/resources/design-layer-allowlist.txt").readLines()
            .map { it.trim() }.filter { it.isNotEmpty() && !it.startsWith("#") }.toSet()
        val offenders = ui.walkTopDown()
            .filter { it.extension == "kt" && it.parentFile != File(ui, "design") }
            .filter { direct.containsMatchIn(it.readText()) }
            .map { it.relativeTo(ui).invariantSeparatorsPath }
            .filterNot { it in allowed }
            .toList()
        assertEquals("use the Ichor* wrapper of ui/design in", emptyList<String>(), offenders)
    }

    @Test
    fun `the guard sees a direct import and lets the rest through`() {
        assertTrue(direct.containsMatchIn("package x\nimport androidx.compose.material3.Card\n"))
        assertTrue(direct.containsMatchIn("import androidx.compose.material3.*"))
        assertFalse(direct.containsMatchIn("import androidx.compose.material3.CardDefaults"))
        assertFalse(direct.containsMatchIn("import name.levis.ichor.ui.design.IchorCard"))
    }
}
