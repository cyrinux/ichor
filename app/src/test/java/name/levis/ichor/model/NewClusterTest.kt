package name.levis.ichor.model

import name.levis.ichor.data.JoinRequest
import name.levis.ichor.data.NewClusterStart
import name.levis.ichor.data.TalosJson
import name.levis.ichor.ui.newcluster.NewClusterForm
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class NewClusterTest {
    private val start = NewClusterStart("d1", "lab", "context: lab\ncontexts:\n  lab:\n    key: c2VjcmV0\n")
    private val request = JoinRequest("192.0.2.10", "", "controlplane", "lab", "/dev/vda", "", newCluster = start)

    @Test
    fun newClusterRunHasItsOwnSteps() {
        assertEquals(JoinPhase.newClusterRun, request.phases)
        assertFalse(request.cloud)
        assertFalse(request.imageRun)
    }

    @Test
    fun timelineFollowsTheGoPhases() {
        val events = listOf("secrets", "applying", "installing", "booted", "bootstrapping").mapIndexed { i, phase ->
            JoinProgress(phase = phase, message = phase, at = i.toLong())
        }
        val steps = joinTimeline(events, finished = false, failed = false, phases = request.phases)
        assertEquals(
            listOf(StepStatus.DONE, StepStatus.DONE, StepStatus.DONE, StepStatus.DONE, StepStatus.CURRENT, StepStatus.PENDING),
            steps.map { it.status },
        )
        assertEquals(JoinPhase.READY, JoinPhase.of("ready"))
    }

    @Test
    fun credentialsStayOutOfToString() {
        assertFalse("c2VjcmV0" in request.toString())
        assertFalse("secret-yaml" in NewClusterDraft(draftId = "d1", talosconfig = "secret-yaml").toString())
    }

    @Test
    fun specDefaultsScheduleOnTheControlPlane() {
        val json = TalosJson.encodeToString(NewClusterSpec.serializer(), NewClusterSpec(name = "lab", endpoint = "192.0.2.10", installDisk = "/dev/vda"))
        val back = TalosJson.decodeFromString(NewClusterSpec.serializer(), json)
        assertTrue(back.allowSchedulingOnControlPlanes)
        assertTrue(NewClusterForm(name = "lab", disk = "/dev/vda").complete)
        assertFalse(NewClusterForm(name = "lab").complete)
    }
}
