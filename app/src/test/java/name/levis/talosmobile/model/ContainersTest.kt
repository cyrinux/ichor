package name.levis.talosmobile.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ContainersTest {

    private fun c(id: String, cpu: Long, pod: String = "p", ns: String = "default", name: String = id, memory: Long = 0, image: String = "img") =
        ContainerInfo(id = id, podNamespace = ns, pod = pod, name = name, image = image, status = "CONTAINER_RUNNING", memory = memory, cpuNanos = cpu)

    @Test
    fun cpuPercentIsCpuNanosOverWallNanos() {
        val previous = ContainerSample(at = 10_000, containers = listOf(c("a", 1_000_000_000), c("b", 0)))
        // 3 s later: a used 1.5 s of CPU, b used 6 s (two cores).
        val current = ContainerSample(at = 13_000, containers = listOf(c("a", 2_500_000_000), c("b", 6_000_000_000)))
        val cpu = containerCpuPercents(previous, current)
        assertEquals(50.0, cpu.getValue("a"), 1e-9)
        assertEquals(200.0, cpu.getValue("b"), 1e-9)
    }

    @Test
    fun noPreviousOrRestartedContainerIsZero() {
        val current = ContainerSample(at = 3_000, containers = listOf(c("new", 9_000_000_000)))
        assertEquals(0.0, containerCpuPercents(null, current).getValue("new"), 0.0)
        // A restarted container has a new id: its counter starts over, no delta yet.
        val previous = ContainerSample(at = 0, containers = listOf(c("old", 8_000_000_000, name = "app")))
        val restarted = ContainerSample(at = 3_000, containers = listOf(c("new", 100, name = "app")))
        assertEquals(0.0, containerCpuPercents(previous, restarted).getValue("new"), 0.0)
    }

    @Test
    fun counterGoingBackwardsOrNoWallTimeIsZero() {
        val previous = ContainerSample(at = 1_000, containers = listOf(c("a", 5_000)))
        assertEquals(0.0, containerCpuPercents(previous, ContainerSample(2_000, listOf(c("a", 4_000)))).getValue("a"), 0.0)
        assertEquals(0.0, containerCpuPercents(previous, ContainerSample(1_000, listOf(c("a", 9_000)))).getValue("a"), 0.0)
    }

    @Test
    fun groupsByPodWithTotalsAndSorts() {
        val rows = listOf(
            ContainerRow(c("1", 0, pod = "web", memory = 100), 10.0),
            ContainerRow(c("2", 0, pod = "web", memory = 300), 5.0),
            ContainerRow(c("3", 0, pod = "db", memory = 1000), 1.0),
            ContainerRow(c("4", 0, pod = "web", ns = "other", memory = 1), 0.0),
        )
        val byCpu = rows.podGroups("", ContainerSort.CPU)
        assertEquals(listOf("default/web", "default/db", "other/web"), byCpu.map { "${it.namespace}/${it.pod}" })
        assertEquals(15.0, byCpu[0].cpuPercent, 1e-9)
        assertEquals(400L, byCpu[0].memory)
        assertEquals(listOf("1", "2"), byCpu[0].containers.map { it.info.id })

        val byMemory = rows.podGroups("", ContainerSort.MEMORY)
        assertEquals(listOf("default/db", "default/web", "other/web"), byMemory.map { "${it.namespace}/${it.pod}" })
        assertEquals(listOf("2", "1"), byMemory[1].containers.map { it.info.id })
    }

    @Test
    fun filterMatchesNamespacePodNameOrImage() {
        val rows = listOf(
            ContainerRow(c("1", 0, pod = "coredns-x", ns = "kube-system", name = "coredns", image = "registry.k8s.io/coredns:1.11"), 0.0),
            ContainerRow(c("2", 0, pod = "web", ns = "shop", name = "nginx", image = "nginx:1.27"), 0.0),
        )
        assertEquals(listOf("1"), rows.podGroups("KUBE-sys", ContainerSort.CPU).flatMap { it.containers }.map { it.info.id })
        assertEquals(listOf("2"), rows.podGroups(" nginx:1.27 ", ContainerSort.CPU).flatMap { it.containers }.map { it.info.id })
        assertTrue(rows.podGroups("nothing", ContainerSort.CPU).isEmpty())
    }

    @Test
    fun runningAndStatusLabel() {
        assertTrue(c("a", 0).running)
        val exited = c("a", 0).copy(status = "CONTAINER_EXITED")
        assertFalse(exited.running)
        assertEquals("exited", exited.statusLabel)
    }
}
