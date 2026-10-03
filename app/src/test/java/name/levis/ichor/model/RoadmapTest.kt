package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import java.io.File

class RoadmapTest {

    private val sample = """
        {
          "currency": "EUR",
          "tips": ["tip_small", "Bad Id", "tip_large"],
          "features": [
            {
              "id": "shipped-one",
              "title": {"en": "Old"},
              "status": "shipped",
              "shippedIn": "1.5.0",
              "products": ["fund_old_2"]
            },
            {
              "id": "open-one",
              "title": {"en": "Open", "fr": "Ouverte"},
              "description": {"en": "Does things"},
              "goal": 200,
              "raised": 50,
              "products": ["fund_open_2", "fund_open_5", "-nope"],
              "issue": "https://github.com/cyrinux/ichor/issues/1"
            },
            {
              "id": "working",
              "title": {"en": "Working"},
              "status": "in_progress",
              "goal": 100,
              "raised": 150,
              "products": ["fund_working_2"],
              "issue": "javascript:alert(1)"
            },
            {"id": "", "title": {"en": "No id"}},
            {"id": "untitled", "title": {"fr": "Sans titre anglais"}},
            {"id": "future", "title": {"en": "Unknown status"}, "status": "someday", "extra": true},
            {"id": "future", "title": {"en": "Repeated id"}},
            {"title": {"en": "Missing id"}},
            {"id": "broken", "title": {"en": null}},
            "not an object"
          ]
        }
    """.trimIndent()

    @Test
    fun decodesAndDropsInvalidEntries() {
        val roadmap = decodeRoadmap(sample)!!
        assertEquals(listOf("working", "open-one", "future", "shipped-one"), roadmap.features.map { it.id })
        assertEquals(listOf("tip_small", "tip_large"), roadmap.tips)
        assertEquals(listOf("fund_open_2", "fund_open_5"), roadmap.features.first { it.id == "open-one" }.products)
    }

    @Test
    fun unknownStatusIsOpen() {
        assertEquals(FeatureStatus.OPEN, decodeRoadmap(sample)!!.features.first { it.id == "future" }.status)
    }

    @Test
    fun onlyHttpsIssueLinksAreKept() {
        val features = decodeRoadmap(sample)!!.features
        assertEquals("https://github.com/cyrinux/ichor/issues/1", features.first { it.id == "open-one" }.issue)
        assertNull(features.first { it.id == "working" }.issue)
    }

    @Test
    fun corruptOrMissingIsNull() {
        assertNull(decodeRoadmap(null))
        assertNull(decodeRoadmap(""))
        assertNull(decodeRoadmap("{not json"))
        assertNull(decodeRoadmap("[]"))
    }

    @Test
    fun localizedFallsBackToEnglish() {
        val title = mapOf("en" to "Open", "fr" to "Ouverte")
        assertEquals("Ouverte", title.localized("fr"))
        assertEquals("Open", title.localized("de"))
        assertEquals("Solo", mapOf("it" to "Solo").localized("de"))
        assertEquals("", emptyMap<String, String>().localized("en"))
    }

    @Test
    fun progressIsClamped() {
        val features = decodeRoadmap(sample)!!.features.associateBy { it.id }
        assertEquals(0.25f, features.getValue("open-one").progress, 0.001f)
        assertEquals(1f, features.getValue("working").progress, 0.001f)
        assertEquals(0f, features.getValue("future").progress, 0.001f)
    }

    @Test
    fun shippedOrProductlessFeaturesAreNotFundable() {
        val features = decodeRoadmap(sample)!!.features.associateBy { it.id }
        assertTrue(features.getValue("open-one").fundable)
        assertTrue(features.getValue("working").fundable)
        assertFalse(features.getValue("shipped-one").fundable)
        assertFalse(features.getValue("future").fundable)
    }

    @Test
    fun productIdsCoverTipsAndFundableFeatures() {
        assertEquals(
            listOf("tip_small", "tip_large", "fund_working_2", "fund_open_2", "fund_open_5"),
            decodeRoadmap(sample)!!.productIds,
        )
    }

    @Test
    fun featureIsBackedWhenAnyOfItsProductsWasBought() {
        val open = decodeRoadmap(sample)!!.features.first { it.id == "open-one" }
        assertTrue(open.backedWith(setOf("fund_open_5")))
        assertFalse(open.backedWith(setOf("fund_working_2")))
    }

    @Test
    fun publishedRoadmapIsValid() {
        // docs/roadmap.json is what the Play build downloads from the website: every entry must survive decoding.
        val text = File("../docs/roadmap.json").readText()
        val declared = Json.parseToJsonElement(text).jsonObject.getValue("features").jsonArray.size
        assertEquals(declared, decodeRoadmap(text)?.features?.size)
    }
}
