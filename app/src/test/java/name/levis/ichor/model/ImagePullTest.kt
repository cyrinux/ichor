package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Test

class ImagePullTest {

    @Test
    fun decodesProgress() {
        val p = TalosJson.decodeFromString(
            ImagePullProgress.serializer(),
            """{"nodes":[{"node":"10.0.0.2","hostname":"cp-1","state":"done"},
               {"node":"10.0.0.3","hostname":"","state":"failed","error":"not found"},
               {"node":"10.0.0.4","hostname":"w-2","state":"pulling"}],"done":1,"total":3,"at":5}""",
        )
        assertEquals(1, p.done)
        assertEquals(3, p.total)
        assertEquals(1, p.failed)
        assertEquals(listOf(ImagePullState.DONE, ImagePullState.FAILED, ImagePullState.PULLING), p.nodes.map { it.pullState })
        assertEquals("10.0.0.3", p.nodes[1].label)
        assertEquals("not found", p.nodes[1].error)
    }

    @Test
    fun unknownStateIsPending() {
        assertEquals(ImagePullState.PENDING, ImagePullState.of("queued"))
    }

    @Test
    fun namespacesMatchTheGoNames() {
        assertEquals(listOf("system", "cri"), ImagePullNamespace.entries.map { it.wire })
    }
}
