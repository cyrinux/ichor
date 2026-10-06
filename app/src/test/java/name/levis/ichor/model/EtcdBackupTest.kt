package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.Date
import java.util.TimeZone

class EtcdBackupTest {

    private fun member(node: String, leader: Boolean = false, learner: Boolean = false, error: String? = null, errors: List<String> = emptyList()) =
        EtcdNodeStatus(node = node, memberId = if (error == null) "id-$node" else "", isLeader = leader, isLearner = learner, error = error, errors = errors)

    @Test
    fun prefersHealthyFollower() {
        val chosen = snapshotCandidates(
            listOf(member("leader", leader = true), member("broken", errors = listOf("corrupt")), member("down", error = "timeout"), member("follower")),
        ).firstOrNull()
        assertEquals("follower", chosen?.node)
    }

    @Test
    fun fallsBackToLeaderThenNothing() {
        assertEquals("leader", snapshotCandidates(listOf(member("leader", leader = true), member("down", error = "x"))).firstOrNull()?.node)
        assertNull(snapshotCandidates(listOf(member("down", error = "x"))).firstOrNull())
    }

    @Test
    fun candidatesListFollowersBeforeLearnersAndLeader() {
        val order = snapshotCandidates(listOf(member("l", leader = true), member("n", learner = true), member("b"), member("a")))
        assertEquals(listOf("a", "b", "n", "l"), order.map { it.node })
    }

    @Test
    fun defaultFileName() {
        val at = Date(1_790_000_000_000) // 2026-09-21T14:13:20Z
        assertEquals("etcd-prod-cp-1-20260921-1413.snapshot", snapshotFileName("prod", "cp-1", at, TimeZone.getTimeZone("UTC")))
        assertEquals("etcd-admin-home-cp-1.lan-20260921-1613.snapshot", snapshotFileName("admin@home", "cp 1.lan", at, TimeZone.getTimeZone("Europe/Paris")))
        assertEquals("etcd-unknown-unknown-20260921-1413.snapshot", snapshotFileName("/", "", at, TimeZone.getTimeZone("UTC")))
        assertEquals("etcd-prod-cp-1-20260921-1413.snapshot.age", snapshotFileName("prod", "cp-1", at, TimeZone.getTimeZone("UTC"), encrypted = true))
    }

    @Test
    fun restoreCommandsDecryptCheckAndRecover() {
        val keys = snapshotRestoreCommands("etcd-prod.snapshot.age", SnapshotMode.KEYS, "abc")
        assertTrue(keys.contains("age -d -i ~/.ssh/id_ed25519 -o etcd.snapshot etcd-prod.snapshot.age"))
        assertTrue(keys.contains("age-yubikey-identity"))
        assertTrue(keys.contains("sha256sum etcd.snapshot  # expect abc"))
        assertTrue(keys.endsWith("bootstrap --recover-from=./etcd.snapshot"))

        val pass = snapshotRestoreCommands("etcd-prod.snapshot.age", SnapshotMode.PASSPHRASE, "abc")
        assertTrue(pass.contains("age -d -o etcd.snapshot etcd-prod.snapshot.age"))
        assertFalse(pass.contains("-i "))

        // A clear snapshot is used as is; a name with spaces or quotes is shell-quoted.
        val clear = snapshotRestoreCommands("my etcd's.snapshot", SnapshotMode.NONE, "abc")
        assertFalse(clear.contains("age "))
        assertTrue(clear.contains("sha256sum 'my etcd'\\''s.snapshot'"))
        assertTrue(clear.endsWith("--recover-from=./'my etcd'\\''s.snapshot'"))
    }

    @Test
    fun passphraseIsNeverPrinted() {
        assertEquals("Passphrase(***)", SnapshotEncryption.Passphrase("correct horse battery").toString())
    }

    @Test
    fun snapshotRoles() {
        assertTrue(ContextSummary(name = "b", roles = listOf("os:etcd:backup")).allows(Feature.ETCD_SNAPSHOT))
        assertTrue(ContextSummary(name = "o", roles = listOf("os:operator")).allows(Feature.ETCD_SNAPSHOT))
        assertFalse(ContextSummary(name = "r", roles = listOf("os:reader")).allows(Feature.ETCD_SNAPSHOT))
        assertTrue(ContextSummary(name = "a", roles = listOf("os:admin")).allows(Feature.MACHINE_CONFIG))
        assertFalse(ContextSummary(name = "o", roles = listOf("os:operator")).allows(Feature.MACHINE_CONFIG))
    }
}
