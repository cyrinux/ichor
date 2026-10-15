package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import name.levis.ichor.monitor.DATA_CRITICAL
import name.levis.ichor.monitor.dataIssuesOf
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class SealedSecretsTest {
    private val json = """
        {"sealedSecrets":{"version":"v1alpha1","error":"","health":"critical","reasons":["keyCompromised"],
         "controller":{"namespace":"kube-system","name":"sealed-secrets-controller","ready":true,"replicas":1,"readyReplicas":1,"health":"ok","reasons":[]},
         "items":[
          {"namespace":"shop","name":"legacy-cert","keys":["tls.crt","tls.key"],"keyCount":2,"type":"kubernetes.io/tls","immutable":false,
           "scope":"strict","synced":false,"reason":"ErrUnsealFailed","message":"no key could decrypt secret","lastTransitionTime":1760000000000,
           "observedGeneration":1,"stale":true,"age":1759900000000,"health":"critical","reasons":["notSynced","stale"]},
          {"namespace":"shop","name":"taken","keys":["password"],"keyCount":1,"type":"Opaque","immutable":false,
           "scope":"strict","synced":false,"reason":"Unmanaged","message":"Resource already exists","lastTransitionTime":1760000000000,
           "observedGeneration":1,"stale":false,"age":1759900000000,"health":"warning","reasons":["unmanaged"]},
          {"namespace":"shop","name":"api-token","keys":["token"],"keyCount":1,"type":"Opaque","immutable":true,
           "scope":"namespace-wide","synced":true,"reason":"","message":"","lastTransitionTime":0,
           "observedGeneration":1,"stale":false,"age":1759900000000,"health":"ok","reasons":[]}],
         "truncated":false,"secretCount":3,
         "keys":[
          {"namespace":"kube-system","name":"sealed-secrets-key-new","state":"active","createdAt":1759400000000,"age":604800000},
          {"namespace":"kube-system","name":"sealed-secrets-key-old","state":"compromised","createdAt":1758000000000,"age":1814400000}]}}
    """.trimIndent()

    private val services = TalosJson.decodeFromString(DataServices.serializer(), json)

    @Test
    fun decodesControllerKeysAndItems() {
        val s = services.sealedSecrets!!
        assertEquals("v1alpha1", s.version)
        assertEquals(ServiceHealth.CRITICAL, s.serviceHealth)
        assertEquals(listOf(SealedSecretsReason.KEY_COMPROMISED), s.reasonList)
        assertEquals("kube-system/sealed-secrets-controller", s.controller!!.label)
        assertTrue(s.controller!!.ready)
        assertEquals(2, s.keys.size)
        assertTrue(s.keys[0].active)
        assertTrue(s.keys[1].compromised)
        assertEquals(3, s.secretCount)
        val failed = s.items[0]
        assertEquals("shop/legacy-cert", failed.label)
        assertFalse(failed.synced)
        assertEquals(listOf(SealedSecretsReason.NOT_SYNCED, SealedSecretsReason.STALE), failed.reasonList)
        assertEquals(1759900000000, failed.createdAt)
        assertEquals("namespace-wide", s.items[2].scope)
        assertTrue(s.items[2].immutable)
        assertEquals(listOf(DataServiceKind.SEALED_SECRETS), services.detected)
    }

    @Test
    fun objectRefOpensTheSealedSecret() {
        assertEquals(
            KubeObjectRef("bitnami.com", "v1alpha1", "sealedsecrets", "SealedSecret", "shop", "legacy-cert", editable = false),
            services.sealedSecrets!!.items[0].objectRef,
        )
    }

    @Test
    fun summaryCountsSecretsAndSectionProblems() {
        // 3 SealedSecrets: the failed and the unmanaged one need a look, plus the compromised key.
        assertEquals(
            ServiceSummary(total = 3, attention = 3, health = ServiceHealth.CRITICAL),
            services.summary(DataServiceKind.SEALED_SECRETS),
        )
    }

    @Test
    fun unreadableSectionIsUnknown() {
        val s = SealedSecretsStatus(error = "forbidden")
        assertEquals(ServiceSummary(0, 0, ServiceHealth.UNKNOWN, "forbidden"), s.summary())
    }

    @Test
    fun alertsCoverFailedUnsealsOnly() {
        assertEquals(mapOf("sealedsecrets|shop/legacy-cert" to DATA_CRITICAL), dataIssuesOf(services))
    }

    @Test
    fun alertsOnControllerDownAndNoActiveKey() {
        val s = SealedSecretsStatus(
            health = "critical",
            reasons = listOf("noKeys", "controllerDown"),
            controller = SealedSecretsController(namespace = "kube-system", name = "sealed-secrets-controller", replicas = 1, health = "critical", reasons = listOf("controllerDown")),
        )
        assertEquals(
            mapOf(
                "sealedsecrets|kube-system/sealed-secrets-controller" to DATA_CRITICAL,
                "sealedsecrets|kube-system/sealed-secrets-key" to DATA_CRITICAL,
            ),
            dataIssuesOf(DataServices(sealedSecrets = s)),
        )
    }

    @Test
    fun alertSystemNamesSealedSecrets() {
        assertEquals(DataServiceKind.SEALED_SECRETS, dataServiceKindOf("sealedsecrets"))
        assertNull(SealedSecretsStatus().controller)
    }
}
