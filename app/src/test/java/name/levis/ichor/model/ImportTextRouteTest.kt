package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ImportTextRouteTest {
    @Test fun onlyKnownGkeCredentialFieldsOpenDiscovery() {
        for (field in listOf("gcpUserCredentialsJson", "gcpServiceAccountJson", "gcpLoginConfigJson")) {
            val route = TalosJson.decodeFromString(ImportTextRoute.serializer(), """{"kind":"credentials","provider":"gke","field":"$field"}""")
            assertTrue(route.isGkeCredential)
        }
        assertFalse(ImportTextRoute(kind = "kubeconfig").isGkeCredential)
        assertFalse(ImportTextRoute(kind = "talosconfig").isGkeCredential)
        assertFalse(ImportTextRoute(kind = "credentials", provider = "gke", field = "unknown").isGkeCredential)
        assertFalse(ImportTextRoute(kind = "credentials", provider = "eks", field = "gcpUserCredentialsJson").isGkeCredential)
    }

    @Test fun awsConfigOpensEksDiscoveryWithItsValues() {
        val route = TalosJson.decodeFromString(
            ImportTextRoute.serializer(),
            """{"kind":"credentials","provider":"eks","field":"awsSsoStartUrl","values":{"awsSsoStartUrl":"https://example.awsapps.com/start","awsSsoRegion":"us-east-1","awsAccountId":"123456789012","awsRoleName":"ReadOnly","awsRegions":"eu-west-1"}}""",
        )
        assertTrue(route.isEksIdentityCenter)
        assertFalse(route.isGkeCredential)
        assertEquals("https://example.awsapps.com/start", route.values[AWS_SSO_START_URL])
        assertTrue(SsoRoleChoice(accountId = "123456789012", roleName = "ReadOnly").matches(route.values))
        assertFalse(SsoRoleChoice(accountId = "123456789012", roleName = "Admin").matches(route.values))
        assertFalse(ImportTextRoute(kind = "credentials", provider = "eks").isEksIdentityCenter)
    }

    @Test fun awsAccessKeysAreRefused() {
        val route = TalosJson.decodeFromString(ImportTextRoute.serializer(), """{"kind":"refused","provider":"eks","field":"awsAccessKeyId"}""")
        assertTrue(route.isAwsAccessKeys)
        assertFalse(route.isEksIdentityCenter)
        assertTrue(route.values.isEmpty())
    }
}
