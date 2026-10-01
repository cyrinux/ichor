package name.levis.talosmobile.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ResourcesTest {

    private val types = listOf(
        ResourceType("Members.cluster.talos.dev", listOf("member", "members"), "cluster"),
        ResourceType("Affiliates.cluster.talos.dev", listOf("affiliate"), "cluster"),
        ResourceType("OSRootSecrets.secrets.talos.dev", listOf("osrootsecret"), "secrets", "sensitive"),
        ResourceType("Links.net.talos.dev", listOf("link", "links"), "network"),
    )

    @Test
    fun groupsByNamespaceSorted() {
        val groups = resourceTypeGroups(types, "")
        assertEquals(listOf("cluster", "network", "secrets"), groups.map { it.namespace })
        assertEquals(listOf("Affiliates.cluster.talos.dev", "Members.cluster.talos.dev"), groups[0].types.map { it.type })
    }

    @Test
    fun searchMatchesTypeAndAliases() {
        assertEquals(listOf("Links.net.talos.dev"), resourceTypeGroups(types, " LINK ").flatMap { it.types }.map { it.type })
        assertEquals(listOf("Members.cluster.talos.dev"), resourceTypeGroups(types, "members.cl").flatMap { it.types }.map { it.type })
        assertTrue(resourceTypeGroups(types, "nope").isEmpty())
    }

    @Test
    fun sensitivity() {
        assertTrue(types[2].sensitive)
        assertFalse(types[0].sensitive)
        assertFalse(isSensitive("non-sensitive"))
        assertFalse(isSensitive(" "))
        assertTrue(isSensitive("Sensitive"))
    }

    @Test
    fun itemsFilterById() {
        val items = listOf(ResourceItem("eth0"), ResourceItem("lo"), ResourceItem("kubespan"))
        assertEquals(listOf("eth0"), items.matching("ETH").map { it.id })
        assertEquals(3, items.matching("  ").size)
    }
}
