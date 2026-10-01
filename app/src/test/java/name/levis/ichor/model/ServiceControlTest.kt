package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ServiceControlTest {

    private fun svc(state: String) = ServiceInfo(id = "kubelet", state = state, health = "healthy")

    @Test
    fun runningServicesCanBeRestartedOrStopped() {
        assertEquals(listOf(ServiceAction.RESTART, ServiceAction.STOP), svc("Running").offeredActions())
    }

    @Test
    fun stoppedServicesCanBeStarted() {
        listOf("Finished", "Failed", "Skipped", "Initialized").forEach {
            assertEquals(it, listOf(ServiceAction.START), svc(it).offeredActions())
        }
    }

    @Test
    fun nothingWhileChangingState() {
        listOf("Preparing", "Waiting", "Starting", "Stopping").forEach {
            assertTrue(it, svc(it).offeredActions().isEmpty())
        }
    }

    @Test
    fun criticalServices() {
        listOf("apid", "trustd", "etcd", "kubelet", "machined", "containerd", "cri").forEach { assertTrue(it, isCriticalService(it)) }
        assertFalse(isCriticalService("udevd"))
        assertFalse(isCriticalService("ext-tailscale"))
    }

    @Test
    fun cliVerbs() {
        assertEquals(listOf("restart", "stop", "start"), ServiceAction.entries.map { it.cli })
    }
}
