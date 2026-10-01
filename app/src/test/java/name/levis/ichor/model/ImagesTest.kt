package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Test

class ImagesTest {

    private val images = listOf(
        ImageInfo("registry.k8s.io/pause:3.10", "sha256:aaaaaaaaaaaaaaaaaaaa", size = 300_000, created = 1_000),
        ImageInfo("ghcr.io/siderolabs/flannel:v0.26", "sha256:bbbbbbbbbbbbbbbbbbbb", size = 30_000_000, created = 3_000),
        ImageInfo("docker.io/library/nginx:1.27", "sha256:cccccccccccccccccccc", size = 70_000_000, created = 2_000),
    )

    @Test
    fun sortsByNameSizeOrCreated() {
        assertEquals(listOf("docker.io/library/nginx:1.27", "ghcr.io/siderolabs/flannel:v0.26", "registry.k8s.io/pause:3.10"), images.filteredSorted("", ImageSort.NAME).map { it.name })
        assertEquals(listOf(70_000_000L, 30_000_000L, 300_000L), images.filteredSorted("", ImageSort.SIZE).map { it.size })
        assertEquals(listOf(3_000L, 2_000L, 1_000L), images.filteredSorted("", ImageSort.CREATED).map { it.created })
    }

    @Test
    fun filtersByNameOrDigest() {
        assertEquals(1, images.filteredSorted(" PAUSE ", ImageSort.NAME).size)
        assertEquals("docker.io/library/nginx:1.27", images.filteredSorted("cccc", ImageSort.NAME).single().name)
        assertEquals(0, images.filteredSorted("redis", ImageSort.NAME).size)
    }

    @Test
    fun totalsAndShortDigest() {
        assertEquals(100_300_000L, images.totalSize)
        assertEquals("sha256:aaaaaaaaaaaa", shortDigest("sha256:aaaaaaaaaaaaaaaaaaaa"))
        assertEquals("0123456789ab", shortDigest("0123456789abcdef"))
        assertEquals("sha256:abc", shortDigest("sha256:abc"))
    }
}
