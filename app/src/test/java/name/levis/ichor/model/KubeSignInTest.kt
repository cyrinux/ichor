package name.levis.ichor.model

import name.levis.ichor.R
import name.levis.ichor.data.TalosJson
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.goErrorText
import name.levis.ichor.ui.uiText
import name.levis.ichor.ui.userMessage
import name.levis.ichor.ui.overview.KubeSignInBanner
import name.levis.ichor.ui.overview.kubeSignInBanner
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class KubeSignInTest {

    @Test
    fun signInNeededReadsTheMethodAndReason() {
        assertEquals(
            SignInNeeded("oidc", "invalid_grant"),
            signInNeeded("kube-sign-in-required: sign in to this cluster (oidc): invalid_grant"),
        )
        assertEquals(SignInNeeded("eks", ""), signInNeeded("kube-sign-in-required: sign in to this cluster (eks)"))
        // Wrapped by what made the call.
        assertEquals(SignInNeeded("gke", "token expired"), signInNeeded("kube nodes: kube-sign-in-required: sign in to this cluster (gke): token expired"))
        assertEquals(SignInNeeded("", "something else"), signInNeeded("kube-sign-in-required: something else"))
        assertNull(signInNeeded("connection refused"))
        assertNull(signInNeeded(null))
        assertEquals(KUBE_SIGN_IN_REQUIRED, name.levis.ichorgo.Ichorgo.KubeSignInRequired)
    }

    @Test
    fun infoDecodesWithOptions() {
        val info = TalosJson.decodeFromString(
            KubeSignInInfo.serializer(),
            """{"method":"eks","kind":"credentials","fields":["awsSsoStartUrl"],"options":[["awsSsoStartUrl","awsSsoRegion"],["awsAccessKeyId","awsSecretAccessKey","awsSessionToken"]],"signedIn":false}""",
        )
        assertEquals(2, info.fieldSets.size)
        assertEquals(R.string.kube_signin_option_aws_sso, fieldSetLabel(info.fieldSets[0]))
        assertEquals(R.string.kube_signin_option_aws_keys, fieldSetLabel(info.fieldSets[1]))

        val gke = TalosJson.decodeFromString(
            KubeSignInInfo.serializer(),
            """{"method":"gke","kind":"credentials","fields":["gcpServiceAccountJson"],"options":[["gcpServiceAccountJson"],["gcpUserCredentialsJson"],["gcpOAuthClientId","gcpOAuthClientSecret"]],"values":{"gcpOAuthClientId":"123-abc.apps.googleusercontent.com","gcpOAuthClientSecret":"s"},"signedIn":false}""",
        )
        assertEquals(R.string.kube_signin_option_gcp_service_account, fieldSetLabel(gke.fieldSets[0]))
        assertEquals(R.string.kube_signin_option_gcp_user, fieldSetLabel(gke.fieldSets[1]))
        assertEquals(R.string.kube_signin_option_gcp_oauth, fieldSetLabel(gke.fieldSets[2]))
        // The OAuth client of the last sign-in reopens its option, filled: one tap to sign in again.
        assertEquals(2, gke.rememberedOption)
        assertTrue(credentialsComplete(gke.fieldSets[2], gke.values))
        assertEquals(FieldKind.SECRET, credentialField("gcpOAuthClientSecret")?.kind)
        // The redirect URL is for a Web client only: a Desktop client signs in without it.
        val oauth = listOf("gcpOAuthClientId", "gcpOAuthClientSecret", "gcpOAuthRedirectUrl")
        assertEquals(true, credentialField("gcpOAuthRedirectUrl")?.optional)
        assertTrue(credentialsComplete(oauth, mapOf("gcpOAuthClientId" to "123-abc.apps.googleusercontent.com", "gcpOAuthClientSecret" to "s")))

        // "Sign in with Google" (a build with a Google client) is a marker the app does not
        // render yet: it decodes, and its option is never complete from typed values.
        val native = TalosJson.decodeFromString(
            KubeSignInInfo.serializer(),
            """{"method":"gke","kind":"credentials","options":[["gcpServiceAccountJson"],["gcpGoogleSignIn"]],"signedIn":false}""",
        )
        assertEquals(listOf("gcpGoogleSignIn"), native.fieldSets[1])
        assertNull(credentialField("gcpGoogleSignIn"))
        assertFalse(credentialsComplete(native.fieldSets[1], emptyMap()))
        assertEquals(R.string.kube_signin_option_google, fieldSetLabel(native.fieldSets[1]))

        val oidc = TalosJson.decodeFromString(KubeSignInInfo.serializer(), """{"method":"oidc","kind":"browser","signedIn":true,"user":"me@example.com","sessionExpires":1700000000}""")
        assertEquals(listOf(emptyList<String>()), oidc.fieldSets)
        assertTrue(oidc.signedIn)
    }

    @Test
    fun rememberedValuesPickTheirOption() {
        val sets = listOf(listOf("awsSsoStartUrl", "awsSsoRegion"), listOf("awsAccessKeyId", "awsSecretAccessKey"))
        val fresh = KubeSignInInfo(method = "eks", kind = KubeSignInInfo.KIND_CREDENTIALS, options = sets)
        assertEquals(0, fresh.rememberedOption)
        assertTrue(fresh.values.isEmpty())

        val decoded = TalosJson.decodeFromString(
            KubeSignInInfo.serializer(),
            """{"method":"eks","kind":"credentials","options":[["awsAccessKeyId","awsSecretAccessKey"],["awsSsoStartUrl","awsSsoRegion"]],"values":{"awsSsoStartUrl":"https://acme.awsapps.com/start","awsSsoRegion":"eu-west-1"},"signedIn":false}""",
        )
        assertEquals(1, decoded.rememberedOption)
        assertTrue(credentialsComplete(decoded.fieldSets[1], decoded.values))
    }

    @Test
    fun promptDecodes() {
        val device = TalosJson.decodeFromString(SignInPrompt.serializer(), """{"kind":"device","url":"https://idp/device?code=AB","userCode":"AB-CD","verificationUrl":"https://idp/device","expiresIn":600}""")
        assertTrue(device.isDevice)
        assertEquals("AB-CD", device.userCode)
        assertFalse(TalosJson.decodeFromString(SignInPrompt.serializer(), """{"kind":"browser","url":"https://idp/auth","redirectPrefix":"http://localhost:8000"}""").isDevice)
    }

    @Test
    fun credentialsSkipOptionalAndEmptyFields() {
        val keys = listOf("awsAccessKeyId", "awsSecretAccessKey", "awsSessionToken")
        val typed = mapOf("awsAccessKeyId" to " AKIA ", "awsSecretAccessKey" to "secret", "awsSessionToken" to "", "awsRegion" to "eu-west-1")

        assertTrue(credentialsComplete(keys, typed))
        assertFalse(credentialsComplete(keys, typed - "awsSecretAccessKey"))
        assertEquals(mapOf("awsAccessKeyId" to "AKIA", "awsSecretAccessKey" to "secret"), credentialsFor(keys, typed))
    }

    @Test
    fun fieldsHaveKinds() {
        assertEquals(FieldKind.JSON, credentialField("gcpServiceAccountJson")?.kind)
        assertEquals(FieldKind.JSON, credentialField("gcpUserCredentialsJson")?.kind)
        assertEquals(FieldKind.SECRET, credentialField("doApiToken")?.kind)
        assertEquals(FieldKind.TEXT, credentialField("awsRegion")?.kind)
        assertEquals(FieldKind.SECRET, credentialField("serviceAccountKey")?.kind)
        assertNull(credentialField("somethingNew"))
    }

    @Test
    fun discoveryFieldsFollowTheProviders() {
        val fields = discoveryFields(
            mapOf(
                "gke" to listOf("gcpServiceAccountJson"),
                "eks" to listOf("awsRegion", "awsAccessKeyId"),
                "unknown" to listOf("x"),
                "rancher" to emptyList(),
            ),
        )
        assertEquals(listOf(DiscoveryProvider.EKS, DiscoveryProvider.GKE), fields.keys.toList())
        assertEquals(listOf("awsRegion", "awsAccessKeyId"), fields[DiscoveryProvider.EKS])

        // GKE takes a service account key or gcloud user credentials; the others their one set.
        val gke = listOf(listOf("gcpServiceAccountJson"), listOf("gcpUserCredentialsJson", "gcpProjects"))
        val options = discoveryOptions(mapOf("gke" to gke, "unknown" to listOf(listOf("x"))), fields)
        assertEquals(gke, options[DiscoveryProvider.GKE])
        assertEquals(listOf(listOf("awsRegion", "awsAccessKeyId")), options[DiscoveryProvider.EKS])
        assertEquals(fields.keys, options.keys)
        // The project IDs are optional: the credential alone is enough to search.
        assertTrue(credentialsComplete(gke[1], mapOf("gcpUserCredentialsJson" to "{}")))
    }

    @Test
    fun importedNamesAreNewAndReplacedContexts() {
        val conflicts = listOf(ImportConflict(0, "eks-1", sameAs = "eks"), ImportConflict(1, "gke-1"))
        val choices = listOf(ImportChoice(0, replace = true), ImportChoice(1), ImportChoice(2, skip = true))

        val names = importedContextNames(setOf("eks", "gke", "talos"), listOf("eks", "gke", "gke-1"), conflicts, choices)

        assertEquals(listOf("eks", "gke-1"), names)
    }

    @Test
    fun bannerShowsForNoSignInOrARefusedCall() {
        val signedOut = KubeSignInInfo(method = "oidc", signedIn = false)
        val signedIn = signedOut.copy(signedIn = true)
        val refused = UiText.Raw("kube-sign-in-required: sign in to this cluster (oidc): refresh token expired")

        assertEquals(KubeSignInBanner("oidc", null), kubeSignInBanner(signedOut, null))
        assertNull(kubeSignInBanner(signedIn, UiText.Raw("timeout")))
        assertNull(kubeSignInBanner(null, null))
        assertEquals(KubeSignInBanner("oidc", SignInNeeded("oidc", "refresh token expired")), kubeSignInBanner(signedIn, refused))
        assertEquals(KubeSignInBanner("eks", SignInNeeded("eks", "")), kubeSignInBanner(null, UiText.SignInRequired("eks")))
    }

    @Test
    fun coreCodeNeverBecomesUserText() {
        val refused = Exception("kube pods: kube-sign-in-required: sign in to this cluster (azure): AADSTS70043")

        assertEquals(UiText.SignInRequired("azure", "AADSTS70043"), refused.uiText())
        assertFalse(refused.userMessage().contains(KUBE_SIGN_IN_REQUIRED))
        assertFalse(goErrorText("kube-sign-in-required: sign in to this cluster (oidc)").contains(KUBE_SIGN_IN_REQUIRED))
        assertEquals("connection refused", goErrorText("connection refused"))
        assertEquals(UiText.Raw("timeout"), Exception("timeout").uiText())
    }
}
