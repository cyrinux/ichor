package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class ClusterGroupsTest {

    private fun kube(name: String, auth: String = "") = ContextSummary(name = name, kind = KIND_KUBE, auth = auth)

    private fun omni(name: String, identity: String, authKey: String) =
        ContextSummary(name = name, omni = true, identity = identity, authKey = authKey, signIn = "omni")

    private val alpha = kube("arn:aws:eks:eu-west-1:111122223333:cluster/alpha", "eks")
    private val beta = kube("arn:aws:eks:us-east-1:444455556666:cluster/beta", "eks")
    private val gamma = kube("gke_sample-project_europe-west1_gamma", "gke")
    private val home = ContextSummary(name = "admin@home")
    private val lab = ContextSummary(name = "admin@lab")
    private val plain = kube("dev")
    private val six = listOf(home, alpha, gamma, lab, beta, plain)

    @Test
    fun aShortListIsNotGrouped() {
        assertNull(clusterGroups(six.take(5), masked = false))
    }

    @Test
    fun groupsFollowTheFirstClusterOfEach() {
        val groups = clusterGroups(six, masked = false)!!
        assertEquals(listOf("talos", "aws:111122223333", "gcp:sample-project", "aws:444455556666", "kubernetes"), groups.map { it.key })
        assertEquals(listOf(null, "11…33", "sample-project", "44…66", null), groups.map { it.owner })
        assertEquals(listOf(home, lab), groups.first().contexts)
        assertEquals(listOf("talos", "aws", "google-cloud", "aws", "kubernetes"), groups.map { it.logo })
    }

    @Test
    fun screenshotModeMergesTheAccounts() {
        val groups = clusterGroups(six, masked = true)!!
        assertEquals(listOf("talos", "aws", "gcp", "kubernetes"), groups.map { it.key })
        assertEquals(listOf(alpha, beta), groups[1].contexts)
        assertEquals(listOf(null, null, null, null), groups.map { it.owner })
    }

    @Test
    fun omniGroupsBySignIn() {
        val one = omni("omni-one", "ops@example.com", "key-a")
        val two = omni("omni-two", "ops@example.com", "key-a")
        val other = omni("omni-three", "dev@example.com", "key-b")
        val groups = clusterGroups(listOf(one, two, other, home, lab, plain), masked = false)!!
        assertEquals(listOf("omni:key-a", "omni:key-b", "talos", "kubernetes"), groups.map { it.key })
        assertEquals(listOf(one, two), groups.first().contexts)
        assertEquals("ops@example.com", groups.first().owner)
        assertEquals(ClusterProvider.OMNI, groups.first().provider)
        assertEquals("omni", clusterGroupKey(one, masked = true).key)
    }

    @Test
    fun otherCloudsGroupByProviderOnly() {
        assertEquals(ClusterGroup("digitalocean", "digital-ocean", ClusterProvider.DIGITALOCEAN), clusterGroupKey(kube("do-sample", "digitalocean"), masked = false))
        assertEquals(ClusterProvider.AZURE, clusterGroupKey(kube("aks-sample", "azure"), masked = false).provider)
        assertEquals(ClusterProvider.RANCHER, clusterGroupKey(kube("rancher-sample", "rancher"), masked = false).provider)
        assertEquals(ClusterGroup("aws", "aws", ClusterProvider.AWS), clusterGroupKey(kube("eks-sample", "eks"), masked = false))
    }

    @Test
    fun demoClustersGoByKind() {
        assertEquals("talos", clusterGroupKey(ContextSummary(name = "demo", demo = true), masked = false).key)
        assertEquals("kubernetes", clusterGroupKey(kube("demo-kube").copy(demo = true), masked = false).key)
    }
}
