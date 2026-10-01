package name.levis.talosmobile.model

import org.junit.Assert.assertEquals
import org.junit.Test

class ProcessesTest {

    private fun proc(pid: Int, cpu: Double, command: String = "cmd$pid", rss: Long = 0, args: String = "") =
        ProcessInfo(pid = pid, cpuTime = cpu, command = command, rss = rss, args = args)

    @Test
    fun cpuPercentIsCpuSecondsOverWallSeconds() {
        val previous = ProcessSample(at = 10_000, processes = listOf(proc(1, 5.0), proc(2, 1.0)))
        val current = ProcessSample(at = 12_000, processes = listOf(proc(1, 6.0), proc(2, 5.0)))
        val cpu = cpuPercents(previous, current)
        assertEquals(50.0, cpu.getValue(1), 1e-9)
        // Multi-threaded: above 100%, shown as is like top.
        assertEquals(200.0, cpu.getValue(2), 1e-9)
    }

    @Test
    fun noPreviousSampleOrNewPidIsZero() {
        val current = ProcessSample(at = 2_000, processes = listOf(proc(1, 3.0)))
        assertEquals(0.0, cpuPercents(null, current).getValue(1), 0.0)
        val previous = ProcessSample(at = 0, processes = listOf(proc(9, 1.0)))
        assertEquals(0.0, cpuPercents(previous, current).getValue(1), 0.0)
    }

    @Test
    fun reusedPidIsZero() {
        val previous = ProcessSample(at = 0, processes = listOf(proc(1, 50.0, "old"), proc(2, 9.0, "same")))
        val current = ProcessSample(at = 2_000, processes = listOf(proc(1, 51.0, "new"), proc(2, 1.0, "same")))
        val cpu = cpuPercents(previous, current)
        assertEquals(0.0, cpu.getValue(1), 0.0) // different command
        assertEquals(0.0, cpu.getValue(2), 0.0) // CPU time went backwards
    }

    @Test
    fun noWallTimeElapsedIsZero() {
        val sample = ProcessSample(at = 1_000, processes = listOf(proc(1, 2.0)))
        assertEquals(0.0, cpuPercents(sample, sample.copy(processes = listOf(proc(1, 3.0)))).getValue(1), 0.0)
    }

    @Test
    fun filterMatchesCommandOrArgsAndSorts() {
        val rows = listOf(
            ProcessRow(proc(1, 0.0, "kubelet", rss = 100, args = "--config /etc/kubernetes"), 5.0),
            ProcessRow(proc(2, 0.0, "etcd", rss = 300), 1.0),
            ProcessRow(proc(3, 0.0, "containerd", rss = 200, args = "--address /run/KUBE.sock"), 9.0),
        )
        assertEquals(listOf(3, 1, 2), rows.filterAndSort("", ProcessSort.CPU).map { it.info.pid })
        assertEquals(listOf(2, 3, 1), rows.filterAndSort("", ProcessSort.MEMORY).map { it.info.pid })
        assertEquals(listOf(3, 1), rows.filterAndSort(" kube ", ProcessSort.CPU).map { it.info.pid })
    }
}
