package name.levis.ichor.data

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import name.levis.ichor.model.ARGO_SYNC_FAILED
import name.levis.ichor.model.ARGO_SYNC_SUCCEEDED
import name.levis.ichor.model.ARGO_SYNC_TERMINATED
import name.levis.ichor.model.FLUX_FOLLOW_FAILED
import name.levis.ichor.model.FLUX_FOLLOW_READY
import name.levis.ichor.model.FLUX_FOLLOW_STILL
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class GitOpsOperationManagerTest {
    private class FakeFollow {
        var progress: (String) -> Unit = {}
        var done: (String, String) -> Unit = { _, _ -> }
        var cancels = 0
        var terminates = 0
        var refuseTerminate: String? = null
        var starts = 0
    }

    private fun manager(argo: FakeFollow = FakeFollow(), flux: FakeFollow = FakeFollow(), started: MutableList<String> = mutableListOf()) =
        GitOpsOperationManager(
            startArgo = { _, _, onProgress, onDone ->
                argo.starts++
                argo.progress = onProgress
                argo.done = onDone
                ArgoSyncHandle(cancel = { argo.cancels++ }, terminate = {
                    argo.terminates++
                    argo.refuseTerminate?.let { throw IllegalStateException(it) }
                })
            },
            startFlux = { _, _, _, onProgress, onDone ->
                flux.starts++
                flux.progress = onProgress
                flux.done = onDone
                FluxReconcileHandle { flux.cancels++ }
            },
            onArgoStarted = { started += "argo" },
            onFluxStarted = { started += "flux" },
            io = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined),
        )

    @Test
    fun anArgoSyncFollowsItsWavesUntilItSucceeds() {
        val argo = FakeFollow()
        val started = mutableListOf<String>()
        val m = manager(argo, started = started)

        assertTrue(m.followArgoSync("argocd", "shop"))
        assertFalse(m.followArgoSync("argocd", "shop"))
        assertEquals(listOf("argo"), started)

        argo.progress("""{"phase":"Running","wave":2,"waves":4,"done":5,"total":12,"at":1}""")
        assertEquals(2, m.argoSync.value?.progress?.wave)
        assertEquals(12, m.argoSync.value?.progress?.total)

        argo.done(ARGO_SYNC_SUCCEEDED, "")
        val run = m.argoSync.value!!
        assertTrue(run.finished)
        assertNull(run.error)
        assertEquals(ARGO_SYNC_SUCCEEDED, run.result)

        // A late callback of the finished run changes nothing.
        argo.progress("""{"phase":"Running","wave":1,"waves":4}""")
        assertEquals(2, m.argoSync.value?.progress?.wave)

        m.dismissArgo()
        assertNull(m.argoSync.value)
        assertTrue(m.followArgoSync("argocd", "shop"))
        assertEquals(2, argo.starts)
    }

    @Test
    fun aFailedSyncIsAFailureWithArgoCdsMessage() {
        val argo = FakeFollow()
        val m = manager(argo)
        m.followArgoSync("argocd", "shop")
        argo.progress("""{"phase":"Running","message":"one or more objects failed to apply"}""")
        argo.done(ARGO_SYNC_FAILED, "")
        assertEquals("one or more objects failed to apply", m.argoSync.value?.error)

        m.dismissArgo()
        m.followArgoSync("argocd", "shop")
        argo.done("", "the Application is gone")
        assertEquals("the Application is gone", m.argoSync.value?.error)
    }

    @Test
    fun terminateIsSentOnceAndARefusalLetsItBeAskedAgain() {
        val argo = FakeFollow()
        val m = manager(argo)
        m.followArgoSync("argocd", "shop")

        argo.refuseTerminate = "forbidden"
        m.terminate()
        assertEquals(1, argo.terminates)
        assertFalse(m.argoSync.value!!.terminating)
        assertEquals("forbidden", m.argoSync.value!!.terminateError)

        argo.refuseTerminate = null
        m.terminate()
        assertTrue(m.argoSync.value!!.terminating)
        m.terminate() // already asked
        assertEquals(2, argo.terminates)

        argo.done(ARGO_SYNC_TERMINATED, "")
        assertTrue(m.argoSync.value!!.finished)
        assertNull(m.argoSync.value!!.error)
        assertFalse(m.argoSync.value!!.terminating)
        m.terminate() // finished: nothing more
        assertEquals(2, argo.terminates)
    }

    @Test
    fun aStarterThatThrowsLeavesNothingFollowed() {
        val m = GitOpsOperationManager(
            startArgo = { _, _, _, _ -> throw IllegalStateException("no cluster") },
            startFlux = { _, _, _, _, _ -> FluxReconcileHandle {} },
        )
        val thrown = runCatching { m.followArgoSync("argocd", "shop") }.exceptionOrNull()
        assertEquals("no cluster", thrown?.message)
        assertNull(m.argoSync.value)
    }

    @Test
    fun aFluxReconcileEndsReadyFailedOrStillReconciling() {
        val flux = FakeFollow()
        val m = manager(flux = flux)

        assertTrue(m.followFluxReconcile("Kustomization", "flux-system", "apps"))
        flux.progress("""{"phase":"reconciling","message":"applying the manifests","at":1}""")
        assertEquals("applying the manifests", m.fluxReconcile.value?.progress?.message)
        flux.progress("""{"phase":"ready","message":"Applied revision: main@sha1:0a1b2c3"}""")
        flux.done(FLUX_FOLLOW_READY, "")
        assertTrue(m.fluxReconcile.value!!.finished)
        assertNull(m.fluxReconcile.value!!.error)

        m.dismissFlux()
        m.followFluxReconcile("HelmRelease", "web", "frontend")
        flux.progress("""{"phase":"failed","message":"install retries exhausted"}""")
        flux.done(FLUX_FOLLOW_FAILED, "")
        assertEquals("install retries exhausted", m.fluxReconcile.value!!.error)

        m.dismissFlux()
        m.followFluxReconcile("HelmRelease", "web", "frontend")
        flux.done(FLUX_FOLLOW_STILL, "")
        assertNull(m.fluxReconcile.value!!.error)
        assertEquals(FLUX_FOLLOW_STILL, m.fluxReconcile.value!!.result)
    }

    @Test
    fun stopFollowingAFluxReconcileEndsItAtOnce() {
        val flux = FakeFollow()
        val m = manager(flux = flux)
        m.followFluxReconcile("Kustomization", "flux-system", "apps")

        m.stopFollowingFlux()
        assertTrue(m.fluxReconcile.value!!.stopped)
        assertTrue(m.fluxReconcile.value!!.finished)
        // The core then reports the cancelled follow: nothing changes.
        flux.done("", "")
        m.stopFollowingFlux()
        assertEquals(1, flux.cancels)
        assertTrue(m.fluxReconcile.value!!.stopped)
    }

    @Test
    fun anArgoSyncAndAFluxReconcileAreFollowedSideBySide() {
        val m = manager()
        assertTrue(m.followArgoSync("argocd", "shop"))
        assertTrue(m.followFluxReconcile("Kustomization", "flux-system", "apps"))
        assertEquals("shop", m.argoSync.value?.hostname)
        assertEquals("apps", m.fluxReconcile.value?.hostname)
    }
}
