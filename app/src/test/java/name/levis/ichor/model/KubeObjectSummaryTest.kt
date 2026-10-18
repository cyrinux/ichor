package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class KubeObjectSummaryTest {

    private val json = """
        {"kind":"Pod","apiVersion":"v1","namespace":"shop","name":"web-1","health":"bad","healthReason":"Ready: ContainersNotReady",
         "phase":"Running","conditions":[{"type":"Ready","status":"False","reason":"ContainersNotReady","lastTransition":5,"tone":"bad"}],
         "owners":[{"via":"owner","group":"apps","version":"v1","resource":"replicasets","kind":"ReplicaSet","namespace":"shop",
                    "name":"web","namespaced":true,"verbs":["get","update"],"controller":true},
                   {"via":"flux","group":"kustomize.toolkit.fluxcd.io","kind":"Kustomization","namespace":"flux-system","name":"apps"},
                   {"via":"helm","kind":"Release","namespace":"shop","name":"web"}],
         "labels":{"app":"web"},"annotations":{},"created":1,"deleting":0,"finalizers":[],
         "highlights":[{"key":"image","value":"web:1"}],
         "events":[{"type":"Warning","reason":"BackOff","message":"restarting","kind":"Pod","namespace":"shop","name":"web-1","count":2,"first":1,"last":2,"source":"kubelet"}],
         "eventsError":"","future":"ignored"}
    """.trimIndent()

    @Test
    fun decodesTheGoSummary() {
        val s = TalosJson.decodeFromString(KubeObjectSummary.serializer(), json)
        assertEquals(CellTone.BAD, s.healthTone)
        assertEquals(CellTone.BAD, s.conditions.single().toneValue)
        assertEquals("BackOff", s.events.single().reason)
        assertEquals("web:1", s.highlights.single().value)
        assertEquals(3, s.owners.size)
    }

    @Test
    fun ownersOpenOnlyWhenDiscoveryKnewThem() {
        val s = TalosJson.decodeFromString(KubeObjectSummary.serializer(), json)
        val (rs, kustomization, release) = s.owners

        val ref = rs.toRef()!!
        assertEquals(KubeObjectRef("apps", "v1", "replicasets", "ReplicaSet", "shop", "web", editable = true), ref)

        assertFalse(kustomization.openable)
        assertNull(kustomization.toRef())

        assertTrue(release.isHelmRelease)
        assertNull(release.toRef())
    }

    @Test
    fun readsTonesLeniently() {
        assertEquals(CellTone.OK, toneOf("ok"))
        assertEquals(CellTone.WARN, toneOf("warn"))
        assertEquals(CellTone.NONE, toneOf("none"))
        assertEquals(CellTone.NONE, toneOf("purple"))
        assertEquals(CellTone.NONE, KubeObjectSummary().healthTone)
    }

    @Test
    fun onlyAnArgoApplicationOrAFluxKustomizationOffersItsDiff() {
        assertTrue(ObjectOwner(via = ObjectOwner.VIA_ARGO, kind = "Application", namespace = "argocd", name = "web").hasGitOpsDiff)
        assertTrue(ObjectOwner(via = ObjectOwner.VIA_FLUX, kind = "Kustomization", namespace = "flux-system", name = "apps").hasGitOpsDiff)
        // Its namespace unknown, a HelmRelease, a Helm release or a plain owner: no diff to open.
        assertFalse(ObjectOwner(via = ObjectOwner.VIA_ARGO, kind = "Application", name = "web").hasGitOpsDiff)
        assertFalse(ObjectOwner(via = ObjectOwner.VIA_FLUX, kind = "HelmRelease", namespace = "web", name = "web").hasGitOpsDiff)
        assertFalse(ObjectOwner(via = ObjectOwner.VIA_HELM, kind = "Release", namespace = "web", name = "web").hasGitOpsDiff)
        assertFalse(ObjectOwner(via = ObjectOwner.VIA_OWNER, kind = "ReplicaSet", namespace = "web", name = "web-1").hasGitOpsDiff)
        assertEquals("Deployment/web/app", KubeDiffResource(group = "apps", kind = "Deployment", namespace = "web", name = "app").focusKey)
    }
}
