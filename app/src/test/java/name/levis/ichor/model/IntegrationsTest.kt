package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Test

class IntegrationsTest {
    private val istio = IntegrationFamily(
        "istio.io",
        listOf(
            IntegrationGroup("networking.istio.io", "v1", listOf("Gateway", "VirtualService")),
            IntegrationGroup("security.istio.io", "v1", listOf("AuthorizationPolicy")),
        ),
    )

    @Test
    fun decodesTheGoReport() {
        val json = """{"families":[{"id":"istio.io","groups":[{"name":"networking.istio.io","version":"v1","kinds":["Gateway"]}]}],"supported":["argoproj.io"]}"""
        val report = TalosJson.decodeFromString(IntegrationReport.serializer(), json)
        assertEquals(listOf("argoproj.io"), report.supported)
        assertEquals("networking.istio.io/v1", report.families.single().groups.single().label)
    }

    @Test
    fun pickingKeepsOnlyTheChosenGroups() {
        assertEquals(listOf("security.istio.io"), istio.picking(setOf("security.istio.io", "kyverno.io")).groups.map { it.name })
        assertEquals(emptyList<IntegrationGroup>(), istio.picking(emptySet()).groups)
        assertEquals("istio.io", istio.picking(emptySet()).id)
    }

    @Test
    fun labelWithoutVersionIsTheGroup() {
        assertEquals("traefik.io", IntegrationGroup("traefik.io").label)
    }
}
