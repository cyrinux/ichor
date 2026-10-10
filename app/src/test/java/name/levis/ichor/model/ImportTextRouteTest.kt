package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ImportTextRouteTest {
    @Test fun onlyKnownGkeCredentialFieldsOpenDiscovery() {
        for (field in listOf("gcpUserCredentialsJson", "gcpServiceAccountJson")) {
            val route = TalosJson.decodeFromString(ImportTextRoute.serializer(), """{"kind":"credentials","provider":"gke","field":"$field"}""")
            assertTrue(route.isGkeCredential)
        }
        assertFalse(ImportTextRoute(kind = "kubeconfig").isGkeCredential)
        assertFalse(ImportTextRoute(kind = "talosconfig").isGkeCredential)
        assertFalse(ImportTextRoute(kind = "credentials", provider = "gke", field = "unknown").isGkeCredential)
        assertFalse(ImportTextRoute(kind = "credentials", provider = "eks", field = "gcpUserCredentialsJson").isGkeCredential)
    }
}
