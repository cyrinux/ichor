package name.levis.ichor.model

import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class LonghornEngineTest {
    private val old = "longhornio/longhorn-engine:v1.12.0"
    private val new = "longhornio/longhorn-engine:v1.13.0"
    private val preview = LonghornEngineUpgradePreview(
        currentImage = old,
        defaultImage = new,
        canUpgrade = true,
        images = listOf(
            LonghornEngineImage(old, "v1.12.0", deployed = true),
            LonghornEngineImage(new, "v1.13.0", deployed = true, default = true),
            LonghornEngineImage("longhornio/longhorn-engine:v1.14.0", "v1.14.0", deployed = false),
            LonghornEngineImage("longhornio/longhorn-engine:v0.9.0", "v0.9.0", deployed = true, incompatible = true),
        ),
    )

    @Test fun acceptsADeployedCompatibleOtherImage() {
        assertTrue(preview.accepts(new))
        assertFalse(preview.accepts(""))
        assertFalse(preview.accepts(old))
        assertFalse(preview.accepts("longhornio/longhorn-engine:v1.14.0"))
        assertFalse(preview.accepts("longhornio/longhorn-engine:v0.9.0"))
        assertFalse(preview.accepts("foreign/engine:v1"))
    }

    @Test fun refusesWhenLonghornRefusesOrTheReadFailed() {
        assertFalse(preview.copy(canUpgrade = false, reason = "The volume is degraded").accepts(new))
        assertFalse(preview.copy(error = "denied").accepts(new))
    }

    @Test fun preselectsTheDefaultOnlyWhenPickable() {
        assertEquals(new, preview.preselected)
        assertEquals("", preview.copy(currentImage = new).preselected)
        assertEquals("", preview.copy(images = preview.images.map { it.copy(deployed = false) }).preselected)
    }

    @Test fun engineUpgradableNeedsAnOldIdleEngineOnASettledVolume() {
        val v = LonghornVolume("pvc-1", state = "attached", engineOutdated = true)
        assertTrue(v.engineUpgradable)
        assertTrue(v.copy(state = "detached").engineUpgradable)
        assertFalse(v.copy(state = "attaching").engineUpgradable)
        assertFalse(v.copy(engineUpgrading = true).engineUpgradable)
        assertFalse(v.copy(engineOutdated = false).engineUpgradable)
    }

    @Test fun decodesTheCoreJsonWithMissingFields() {
        val json = Json { ignoreUnknownKeys = true }
        val p = json.decodeFromString(
            LonghornEngineUpgradePreview.serializer(),
            """{"name":"pvc-1","live":true,"canUpgrade":true,"images":[{"image":"$new","default":true,"deployed":true}]}""",
        )
        assertTrue(p.live)
        assertEquals(new, p.preselected)
        val status = json.decodeFromString(
            LonghornStatus.serializer(),
            """{"defaultEngineImage":"$new","engineImages":[{"image":"$new","version":"v1.13.0","refCount":1},{"image":"$old","refCount":2}],
               "volumes":[{"name":"a","engineOutdated":true,"engineVersion":"v1.12.0"},{"name":"b"}]}""",
        )
        assertEquals("v1.13.0", status.defaultEngineVersion)
        assertEquals(1, status.oldEngineVolumes)
    }

    @Test fun tagOfAnImage() {
        assertEquals("v1.12.0", engineImageTag(old))
        assertEquals("engine", engineImageTag("registry:5000/engine"))
        assertEquals("", engineImageTag(""))
    }
}
