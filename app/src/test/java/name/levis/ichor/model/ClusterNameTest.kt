package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class ClusterNameTest {

    private val prod = ContextSummary(name = "admin@talos-prod-eu-west-1", fingerprint = "fp-prod")
    private val lab = ContextSummary(name = "admin@lab", fingerprint = "fp-lab")

    @Test
    fun givenNameReplacesTheContextName() {
        val labels = ClusterLabels(mapOf("fp-prod" to "Prod"))
        assertEquals("Prod", labels.of(prod))
        assertEquals("admin@lab", labels.of(lab))
    }

    @Test
    fun screenshotModeShowsTheMaskedContextName() {
        val labels = ClusterLabels(mapOf("fp-prod" to "Prod"), masked = true)
        assertEquals("admin@talos-prod-eu-west-1", labels.of(prod))
        assertNull(labels.given(prod))
    }

    @Test
    fun noFingerprintNoGivenName() {
        val anonymous = ContextSummary(name = "admin@old")
        assertEquals("admin@old", ClusterLabels(mapOf("" to "Old")).of(anonymous))
    }

    @Test
    fun typedNamesAreTrimmedAndBlankResets() {
        assertEquals("Prod", normalizeClusterName("  Prod "))
        assertNull(normalizeClusterName("   "))
        assertEquals(CLUSTER_NAME_MAX, normalizeClusterName("x".repeat(100))!!.length)
    }

    @Test
    fun namesOfRemovedClustersAreForgotten() {
        val saved = mapOf("fp-prod" to "Prod", "fp-gone" to "Gone")
        assertEquals(mapOf("fp-prod" to "Prod"), keepClusterNames(saved, listOf("fp-prod", "fp-lab")))
    }

    private val eks = ContextSummary(name = "arn:aws:eks:eu-north-1:731942086515:cluster/orbit-prod-eu", fingerprint = "fp-eks")
    private val gke = ContextSummary(name = "gke_lumen-sandbox-42_europe-west4_lumen-gke-blue", fingerprint = "fp-gke")

    @Test
    fun eksArnIsParsed() {
        val eks = parseCloudContext(eks.name)!!
        assertEquals(CloudContext.EKS, eks.provider)
        assertEquals("eu-north-1", eks.location)
        assertEquals("731942086515", eks.owner)
        assertEquals("orbit-prod-eu", eks.cluster)
        assertEquals("73…15", eks.shortOwner)
        assertEquals("cn-north-1", parseCloudContext("arn:aws-cn:eks:cn-north-1:123456789012:cluster/c")?.location)
        assertEquals("c", parseCloudContext("arn:aws-us-gov:eks:us-gov-west-1:123456789012:cluster/c")?.cluster)
    }

    @Test
    fun gkeContextIsParsed() {
        val gke = parseCloudContext(gke.name)!!
        assertEquals(CloudContext.GKE, gke.provider)
        assertEquals("europe-west4", gke.location)
        assertEquals("lumen-sandbox-42", gke.owner)
        assertEquals("lumen-gke-blue", gke.cluster)
        // A project ID is a name, not a number: shown whole.
        assertEquals("lumen-sandbox-42", gke.shortOwner)
        assertEquals("europe-west1-b", parseCloudContext("gke_proj_europe-west1-b_c")?.location)
        assertEquals("example.com:proj", parseCloudContext("gke_example.com:proj_us-east1_c")?.owner)
    }

    @Test
    fun otherNamesAreNoCloudContext() {
        assertNull(parseCloudContext("admin@lab"))
        assertNull(parseCloudContext("arn:aws:iam::123456789012:role/admin"))
        assertNull(parseCloudContext("arn:aws:eks:eu-north-1:123456789012:cluster/"))
        assertNull(parseCloudContext("arn:aws:eks:eu-north-1:account:cluster/c"))
        assertNull(parseCloudContext("gke_proj_us-central1"))
        assertNull(parseCloudContext("gke_proj_us-central1_my_cluster"))
    }

    @Test
    fun cloudContextIsCalledByItsClusterName() {
        assertEquals("orbit-prod-eu", ClusterLabels().of(eks))
        assertEquals("lumen-gke-blue", ClusterLabels().of(gke))
        assertEquals("eu-north-1", ClusterLabels().cloud(eks)?.location)
        assertEquals("Prod", ClusterLabels(mapOf("fp-eks" to "Prod")).of(eks))
    }

    @Test
    fun screenshotModeParsesNoContextName() {
        val labels = ClusterLabels(masked = true)
        assertEquals(eks.name, labels.of(eks))
        assertNull(labels.cloud(eks))
        assertNull(labels.cloud(gke))
    }

    private fun kube(name: String = "ctx", auth: String = "cert", server: String = "https://10.0.0.1:6443") =
        ContextSummary(name = name, kind = KIND_KUBE, auth = auth, endpoints = listOf(server))

    @Test
    fun logoOfATalosClusterIsTalos() {
        assertEquals("talos", clusterLogo(prod))
    }

    @Test
    fun logoFollowsTheSignInMethod() {
        assertEquals("aws", clusterLogo(kube(auth = "eks")))
        assertEquals("google-cloud", clusterLogo(kube(auth = "gke")))
        assertEquals("azure", clusterLogo(kube(auth = "azure")))
        assertEquals("digital-ocean", clusterLogo(kube(auth = "digitalocean")))
        assertEquals("rancher", clusterLogo(kube(auth = "rancher")))
    }

    @Test
    fun staticCredentialsFallBackOnTheNameThenTheHost() {
        assertEquals("aws", clusterLogo(kube(name = eks.name, auth = "token")))
        assertEquals("google-cloud", clusterLogo(kube(name = gke.name, auth = "cert")))
        assertEquals("aws", clusterLogo(kube(server = "https://ABCD.gr7.eu-west-1.eks.amazonaws.com")))
        assertEquals("azure", clusterLogo(kube(server = "https://aks-dns-1234.hcp.westeurope.azmk8s.io:443")))
        assertEquals("digital-ocean", clusterLogo(kube(server = "https://1234-abcd.k8s.ondigitalocean.com")))
        assertEquals("kubernetes", clusterLogo(kube()))
        assertEquals("kubernetes", clusterLogo(kube(auth = "oidc")))
    }

    @Test
    fun discoveryProvidersShowTheirCloudLogo() {
        assertEquals(
            listOf("aws", "google-cloud", "azure", "digital-ocean", "rancher"),
            DiscoveryProvider.entries.map { discoveryLogo(it.id) },
        )
        assertEquals("kubernetes", discoveryLogo("other"))
    }
}
