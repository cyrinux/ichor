package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Test

class LogSourceTest {

    @Test
    fun containerTitleAndSubtitle() {
        val c = ContainerInfo(id = "0123456789abcdef0123", podNamespace = "kube-system", pod = "coredns-1", name = "coredns")
        assertEquals("coredns", containerLogTitle(c))
        assertEquals("kube-system/coredns-1", containerLogSubtitle(c))
        assertEquals("0123456789ab", containerLogTitle(c.copy(name = "")))
        assertEquals("coredns-1", containerLogSubtitle(c.copy(podNamespace = "")))
        assertEquals("", containerLogSubtitle(c.copy(podNamespace = "", pod = "")))
    }

    @Test
    fun keysTellSourcesApart() {
        assertEquals("kernel", LogSource.Service(null).key)
        assertEquals("kubelet", LogSource.Service("kubelet").key)
        assertNotEquals(LogSource.Service("abc").key, LogSource.Container("abc", "t", "s").key)
    }
}
