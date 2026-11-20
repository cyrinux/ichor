package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class KubeBastionTest {

    @Test
    fun eachCodeIsItsOwnProblemWhereverItSits() {
        assertEquals(BastionProblem.UNREACHABLE, bastionProblem("kube-bastion-unreachable: dial tcp bastion.example.net:22: i/o timeout"))
        assertEquals(BastionProblem.HOST_KEY, bastionProblem("kube nodes: kube-bastion-host-key: the bastion presented another host key"))
        assertEquals(BastionProblem.AUTH, bastionProblem("kube-bastion-auth: ssh: unable to authenticate"))
        assertEquals(BastionProblem.KEY, bastionProblem("kube-bastion-key: the private key needs its passphrase"))
        assertEquals(BastionProblem.FORWARD, bastionProblem("pods: kube-bastion-forward: administratively prohibited"))
    }

    @Test
    fun otherFailuresAreNotBastionProblems() {
        assertNull(bastionProblem(null))
        assertNull(bastionProblem(""))
        assertNull(bastionProblem("kube-sign-in-required: sign in to this cluster (oidc)"))
        // Refused before any connection: a setting, not a broken hop.
        assertNull(bastionProblem("kube-bastion-unavailable: this cluster cannot go through an SSH bastion (proxy-url)"))
        assertNull(bastionProblem("dial tcp 10.0.0.1:6443: connect: connection refused"))
    }

    @Test
    fun theInfoDecodesWithoutSecrets() {
        val info = TalosJson.decodeFromString(
            KubeBastionInfo.serializer(),
            """{"configured":true,"host":"bastion.example.net:22","user":"jump","auth":"key",
               "hostKeyType":"ssh-ed25519","hostKeyFingerprint":"SHA256:abc","unavailable":""}""",
        )
        assertTrue(info.configured && info.usesKey)
        assertEquals("SHA256:abc", info.hostKeyFingerprint)

        val none = TalosJson.decodeFromString(KubeBastionInfo.serializer(), """{"configured":false,"unavailable":"proxy-url"}""")
        assertFalse(none.configured)
        assertEquals("proxy-url", none.unavailable)
    }

    @Test
    fun emptyFieldsAreLeftOutSoTheStoredOnesStay() {
        val edit = BastionFields(host = " bastion.example.net ", user = "jump")
        assertEquals(mapOf("host" to "bastion.example.net", "user" to "jump"), edit.toMap())
        val pin = BastionFields(hostKey = "ssh-ed25519 AAAA")
        assertEquals(mapOf("hostKey" to "ssh-ed25519 AAAA"), pin.toMap())
        // A password is kept exactly as typed.
        assertEquals(" pw ", BastionFields(password = " pw ").toMap()["password"])
    }

    @Test
    fun aKubeconfigClusterWithoutProxyAndATalosClusterOffOmniAreOffered() {
        val kube = ContextSummary("jump", kind = KIND_KUBE)
        assertTrue(bastionOffered(kube, masked = false))
        assertFalse(bastionOffered(kube, masked = true))
        assertFalse(bastionOffered(kube.copy(proxyURL = "socks5://127.0.0.1:1080"), masked = false))
        assertFalse(bastionOffered(kube.copy(demo = true), masked = false))
        val talos = ContextSummary("lab")
        assertTrue(bastionOffered(talos, masked = false))
        assertFalse(bastionOffered(talos, masked = true))
        assertFalse(bastionOffered(talos.copy(omni = true), masked = false))
        assertFalse(bastionOffered(talos.copy(demo = true), masked = false))
    }

    @Test
    fun aTalosClusterCarriesItsScope() {
        val info = TalosJson.decodeFromString(KubeBastionInfo.serializer(), """{"configured":true,"unavailable":"","scope":"kube"}""")
        assertTrue(info.talos)
        assertEquals(KubeBastionInfo.SCOPE_KUBE, info.scope)
        assertFalse(TalosJson.decodeFromString(KubeBastionInfo.serializer(), """{"configured":false,"unavailable":""}""").talos)
        assertEquals("all", BastionFields(scope = KubeBastionInfo.SCOPE_ALL).toMap()["scope"])
        assertFalse("scope" in BastionFields(host = "b").toMap())
    }
}
