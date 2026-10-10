package name.levis.ichor.data

import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File
import java.util.zip.ZipEntry
import java.util.zip.ZipFile
import java.util.zip.ZipOutputStream

class SupportBundleZipTest {

    @get:Rule
    val tmp = TemporaryFolder()

    private fun zip(file: File, vararg entries: Pair<String, String>) {
        ZipOutputStream(file.outputStream()).use { out ->
            entries.forEach { (name, text) ->
                out.putNextEntry(ZipEntry(name))
                out.write(text.toByteArray())
                out.closeEntry()
            }
        }
    }

    private fun read(file: File): Map<String, String> = ZipFile(file).use { zip ->
        zip.entries().asSequence().associate { it.name to zip.getInputStream(it).readBytes().toString(Charsets.UTF_8) }
    }

    @Test
    fun theHistoryIsAddedNextToWhatTheBundleHolds() {
        val bundle = tmp.newFile("support.zip")
        zip(bundle, "cluster/gitops.json" to "{}", "10.0.0.11/dmesg.log" to "boot")

        addZipEntry(bundle, SupportBundleRepository.HISTORY_ENTRY, """{"format":1}""".toByteArray())

        assertEquals(
            mapOf("cluster/gitops.json" to "{}", "10.0.0.11/dmesg.log" to "boot", "cluster/history.json" to """{"format":1}"""),
            read(bundle),
        )
        assertEquals(listOf("support.zip"), tmp.root.list()!!.toList())
    }

    @Test
    fun anEntryOfTheSameNameIsReplaced() {
        val bundle = tmp.newFile("support.zip")
        zip(bundle, "cluster/history.json" to "old")

        addZipEntry(bundle, "cluster/history.json", "new".toByteArray())

        assertEquals(mapOf("cluster/history.json" to "new"), read(bundle))
    }
}
