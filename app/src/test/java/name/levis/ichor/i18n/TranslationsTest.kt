package name.levis.ichor.i18n

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.w3c.dom.Element
import java.io.File
import javax.xml.parsers.DocumentBuilderFactory

/** Every translation has exactly the default strings, with the same format placeholders. */
class TranslationsTest {

    private val res = File("src/main/res")
    private val languages = listOf("fr", "es", "uk", "de", "it")

    /** One `<string>` (single text) or `<plurals>` (one text per quantity). */
    private data class Entry(val texts: Map<String, String>) {
        val placeholders: Set<String> get() = texts.values.flatMap { PLACEHOLDER.findAll(it).map { m -> m.value } }.toSet()
        val isPlural: Boolean get() = !texts.containsKey("")
    }

    private fun load(dir: String): Map<String, Entry> {
        val file = File(res, "$dir/strings.xml")
        assertTrue("missing $file", file.isFile)
        val doc = DocumentBuilderFactory.newInstance().newDocumentBuilder().parse(file)
        val entries = mutableMapOf<String, Entry>()
        val children = doc.documentElement.childNodes
        for (i in 0 until children.length) {
            val el = children.item(i) as? Element ?: continue
            if (el.getAttribute("translatable") == "false") continue
            val name = el.getAttribute("name")
            entries[name] = when (el.tagName) {
                "string" -> Entry(mapOf("" to el.textContent))
                "plurals" -> {
                    val items = el.getElementsByTagName("item")
                    Entry((0 until items.length).associate { j ->
                        val item = items.item(j) as Element
                        item.getAttribute("quantity") to item.textContent
                    })
                }
                else -> continue
            }
        }
        return entries
    }

    private val default by lazy { load("values") }

    @Test
    fun defaultHasStrings() {
        assertTrue(default.size > 100)
    }

    @Test
    fun everyKeyIsTranslated() {
        languages.forEach { lang ->
            val missing = default.keys - load("values-$lang").keys
            assertEquals("values-$lang is missing keys", emptySet<String>(), missing)
        }
    }

    @Test
    fun noExtraKeysInTranslations() {
        languages.forEach { lang ->
            val extra = load("values-$lang").keys - default.keys
            assertEquals("values-$lang has keys not in values/", emptySet<String>(), extra)
        }
    }

    @Test
    fun placeholdersMatch() {
        languages.forEach { lang ->
            load("values-$lang").forEach { (name, entry) ->
                val expected = default[name]?.placeholders ?: return@forEach
                entry.texts.forEach { (quantity, text) ->
                    val found = PLACEHOLDER.findAll(text).map { it.value }.toSet()
                    assertEquals("values-$lang/$name[$quantity]", expected, found)
                }
            }
        }
    }

    @Test
    fun pluralsHaveOther() {
        (listOf("values") + languages.map { "values-$it" }).forEach { dir ->
            load(dir).filterValues { it.isPlural }.forEach { (name, entry) ->
                assertTrue("$dir/$name has no \"other\" item", "other" in entry.texts)
            }
        }
    }

    @Test
    fun ukrainianPluralsHaveAllQuantities() {
        load("values-uk").filterValues { it.isPlural }.forEach { (name, entry) ->
            assertTrue("values-uk/$name: ${entry.texts.keys}", entry.texts.keys.containsAll(listOf("one", "few", "many", "other")))
        }
    }

    private companion object {
        val PLACEHOLDER = Regex("""%\d+\$[sd]""")
    }
}
