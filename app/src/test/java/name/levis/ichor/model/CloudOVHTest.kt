package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class CloudOVHTest {
    private val options = TalosJson.decodeFromString(
        OVHOptions.serializer(),
        """
        {"project":"scratch","currency":"EUR","regions":["DE1","GRA11"],
         "flavors":[{"id":"a","name":"b3-8","region":"GRA11","vcpus":2,"ramGB":8,"diskGB":50,"hourly":"0.0512"},
                    {"id":"b","name":"b3-8","region":"DE1","vcpus":2,"ramGB":8,"diskGB":50,"hourly":""}],
         "images":[{"id":"t","name":"Talos","region":"GRA11","private":true,"talos":true},
                   {"id":"d","name":"Debian","region":"DE1","private":false,"talos":false}]}
        """,
    )

    @Test
    fun aRegionListsOnlyItsOwn() {
        assertEquals(listOf("a"), options.flavorsIn("GRA11").map { it.id })
        assertEquals(listOf("t"), options.imagesIn("GRA11").map { it.id })
        assertFalse(options.imagesIn("DE1").any { it.talos })
        assertTrue(options.flavorsIn("elsewhere").isEmpty())
    }

    @Test
    fun aFlavorWithoutACatalogHasNoPrice() {
        assertEquals("0.0512", options.flavorsIn("GRA11").single().hourly)
        assertEquals("", options.flavorsIn("DE1").single().hourly)
    }

    @Test
    fun theCheckTellsAnUncheckedRightApart() {
        val check = TalosJson.decodeFromString(OVHCheck.serializer(), """{"project":"scratch","projects":["scratch"],"canCreate":false,"checked":false}""")
        assertFalse(check.checked)
        assertEquals(listOf("scratch"), check.projects)
    }
}
