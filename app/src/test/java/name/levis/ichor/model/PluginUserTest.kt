package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * The imported plugins as the Go core lists them (go/plugin/testdata/user, the golden of
 * TestPluginUserListMatchesGolden, read by the iOS tests too) and the consent sheet built from them.
 */
class PluginUserTest {
    private val list: PluginUserList = TalosJson.decodeFromString(
        PluginUserList.serializer(),
        File("../go/plugin/testdata/user/plugin-user-list.json").readText(),
    )

    @Test
    fun goldenDecodes() {
        assertEquals(listOf("old-format", "sleep-copy"), list.items.map { it.id })
        val broken = list.items[0]
        assertFalse(broken.usable)
        assertFalse(broken.enabled)
        assertTrue(broken.error, broken.error.contains("not supported"))
        val copy = list.items[1]
        assertTrue(copy.usable)
        assertTrue(copy.enabled)
        assertEquals("kube-green", copy.name)
    }

    @Test
    fun consentListsReadsAndActions() {
        val consent = list.items[1].consent()
        assertEquals(listOf("sleepinfos.kube-green.com", "secrets"), consent.resources.map { it.name })
        assertEquals(listOf(false, true), consent.resources.map { it.secret })
        assertTrue(consent.revealsSecrets)
        assertFalse(consent.readOnly)
        assertEquals(listOf(PluginConsentAction("Wake up now", "mergePatch", "patch sleepinfos")), consent.actions)
    }

    @Test
    fun maskedSecretIsNotFlagged() {
        val item = PluginUserItem(
            id = "kube-green-copy",
            replaces = true,
            resources = listOf(
                PluginCatalogResource(group = "kube-green.com", resource = "sleepinfos", kind = "SleepInfo"),
                PluginCatalogResource(resource = "secrets", kind = "Secret"),
                // Not a core Secret: reveal says nothing here.
                PluginCatalogResource(group = "example.com", resource = "secrets", reveal = true),
            ),
        )
        val consent = item.consent()
        assertEquals("kube-green-copy", consent.name)
        assertTrue(consent.replaces)
        assertFalse(consent.revealsSecrets)
        assertTrue(consent.readOnly)
        assertEquals(listOf(false, true, false), consent.resources.map { it.secret })
    }

    @Test
    fun catalogSplitsBuiltins() {
        val catalog = PluginCatalog(listOf(PluginCatalogItem(id = "kube-green"), PluginCatalogItem(id = "mine", user = true)))
        assertEquals(listOf("kube-green"), catalog.builtins.map { it.id })
    }
}
