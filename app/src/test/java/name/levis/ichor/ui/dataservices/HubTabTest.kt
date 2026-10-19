package name.levis.ichor.ui.dataservices

import name.levis.ichor.model.CastAIStatus
import name.levis.ichor.model.CertManagerStatus
import name.levis.ichor.model.DataServiceKind
import name.levis.ichor.model.DataServices
import name.levis.ichor.model.LonghornStatus
import name.levis.ichor.model.PluginDetectItem
import name.levis.ichor.model.VeleroStatus
import org.junit.Assert.assertEquals
import org.junit.Test

class HubTabTest {
    /** The pager keeps its page keys in a Bundle: they must be Strings, never a HubTab. */
    @Test
    fun pagesAreStringIds() {
        val tabs = listOf(HubTab.Native(DataServiceKind.LONGHORN), HubTab.Plugin("kube-green", "kube-green"))
        val pages: List<Any> = hubPages(tabs)
        assertEquals(listOf("LONGHORN", "plugin:kube-green"), pages)
        pages.forEach { assertEquals(String::class.java, it.javaClass) }
        assertEquals(pages.size, pages.toSet().size)
    }

    private val services = DataServices(longhorn = LonghornStatus(), certManager = CertManagerStatus(), velero = VeleroStatus(), castai = CastAIStatus())
    private val plugins = listOf(PluginDetectItem(id = "sample-db", name = "Sample DB"))

    /** On a Kubernetes cluster the home files cert-manager and CAST AI in their families: the hub keeps storage and backups. */
    @Test
    fun kubernetesHubListsStorageAndPlugins() {
        assertEquals(
            listOf(HubTab.Native(DataServiceKind.LONGHORN), HubTab.Native(DataServiceKind.VELERO), HubTab.Plugin("sample-db", "Sample DB")),
            hubTabs(services, plugins, kube = true, solo = null),
        )
    }

    /** A Talos cluster has no tools card yet: every system stays a tab. */
    @Test
    fun talosHubListsEverySystem() {
        assertEquals(
            listOf(DataServiceKind.LONGHORN, DataServiceKind.CERT_MANAGER, DataServiceKind.VELERO, DataServiceKind.CASTAI).map { HubTab.Native(it) } +
                HubTab.Plugin("sample-db", "Sample DB"),
            hubTabs(services, plugins, kube = false, solo = null),
        )
    }

    @Test
    fun soloShowsThatSystemAloneWhenItRuns() {
        assertEquals(listOf(HubTab.Native(DataServiceKind.CERT_MANAGER)), hubTabs(services, plugins, kube = true, solo = DataServiceKind.CERT_MANAGER))
        assertEquals(emptyList<HubTab>(), hubTabs(services, plugins, kube = true, solo = DataServiceKind.EXTERNAL_DNS))
    }
}
