package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class EtcdRecoverTest {

    @Test
    fun decodesSnapshotInfo() {
        val info = TalosJson.decodeFromString(
            SnapshotInfo.serializer(),
            """{"encrypted":true,"recipientsHint":"scrypt","size":4096,"sha256":"ab12"}""",
        )
        assertEquals(SnapshotOpener.PASSPHRASE, info.opener)
        assertEquals(4096, info.size)
    }

    @Test
    fun openerFollowsTheHint() {
        assertEquals(SnapshotOpener.NONE, SnapshotInfo(encrypted = false).opener)
        assertEquals(SnapshotOpener.SECRET_KEY, SnapshotInfo(encrypted = true, recipientsHint = "x25519").opener)
        assertEquals(SnapshotOpener.UNSUPPORTED, SnapshotInfo(encrypted = true, recipientsHint = "unknown").opener)
    }

    @Test
    fun decodesProgress() {
        val p = TalosJson.decodeFromString(
            EtcdRecoverProgress.serializer(),
            """{"phase":"uploading","message":"uploading the snapshot to 10.0.0.2","at":5,"bytes":1024,"total":4096}""",
        )
        assertEquals("uploading", p.phase)
        assertEquals(1024, p.bytes)
        assertEquals(4096, p.total)
    }

    @Test
    fun timelineSkipsDecryptingForAClearFile() {
        val events = listOf(EtcdRecoverProgress(phase = "uploading"), EtcdRecoverProgress(phase = "bootstrapping"))
        assertEquals(
            listOf(EtcdRecoverPhase.UPLOADING to StepStatus.DONE, EtcdRecoverPhase.BOOTSTRAPPING to StepStatus.CURRENT, EtcdRecoverPhase.WAITING to StepStatus.PENDING),
            etcdRecoverTimeline(events, finished = false, failed = false, encrypted = false),
        )
        assertEquals(
            listOf(StepStatus.DONE, StepStatus.DONE, StepStatus.FAILED, StepStatus.PENDING),
            etcdRecoverTimeline(events, finished = true, failed = true, encrypted = true).map { it.second },
        )
        assertEquals(
            List(4) { StepStatus.DONE },
            etcdRecoverTimeline(events, finished = true, failed = false, encrypted = true).map { it.second },
        )
    }

    @Test
    fun lostOnlyWhenNoMemberAnswers() {
        val down = EtcdNodeStatus(node = "10.0.0.2", error = "etcd is not running")
        val up = EtcdNodeStatus(node = "10.0.0.3", memberId = "a1")
        assertTrue(EtcdOverview(statuses = listOf(down, down.copy(node = "10.0.0.4"))).etcdLost)
        assertFalse(EtcdOverview(statuses = listOf(down, up)).etcdLost)
        assertFalse(EtcdOverview().etcdLost)
        assertEquals(listOf("10.0.0.2", "10.0.0.3"), EtcdOverview(statuses = listOf(down, up)).recoverCandidates)
    }
}
