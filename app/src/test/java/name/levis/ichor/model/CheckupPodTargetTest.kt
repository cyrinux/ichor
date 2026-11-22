package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class CheckupPodTargetTest {

    @Test
    fun everyPodKindOpensItsPod() {
        val kinds = listOf(
            CheckupKind.POD_CRASH_LOOP,
            CheckupKind.POD_IMAGE_PULL,
            CheckupKind.POD_UNSCHEDULABLE,
            CheckupKind.POD_STUCK_STARTING,
            CheckupKind.POD_NOT_READY,
            CheckupKind.POD_FAILED,
            CheckupKind.POD_OOM_KILLED,
        )
        kinds.forEach { kind ->
            assertEquals(kind, "shop" to "web-1", CheckupFinding(kind, namespace = "shop", name = "web-1").podTarget)
        }
        assertEquals(kinds.toSet(), POD_FINDING_KINDS)
    }

    @Test
    fun otherKindsAndBlankSidesOpenNothing() {
        // A job, a volume or an event finding is not a pod's.
        assertNull(CheckupFinding(CheckupKind.JOB_FAILED, namespace = "shop", name = "migrate").podTarget)
        assertNull(CheckupFinding(CheckupKind.PVC_PENDING, namespace = "shop", name = "data").podTarget)
        assertNull(CheckupFinding(CheckupKind.EVENT, namespace = "shop", name = "web-1", extra = "Pod").podTarget)
        assertNull(CheckupFinding(CheckupKind.POD_TERMINATING, namespace = "shop", name = "web-1").podTarget)
        assertNull(CheckupFinding(CheckupKind.POD_CRASH_LOOP, namespace = "", name = "web-1").podTarget)
        assertNull(CheckupFinding(CheckupKind.POD_CRASH_LOOP, namespace = "shop", name = "").podTarget)
    }
}
