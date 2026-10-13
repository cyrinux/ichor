package name.levis.ichor.ui.etcd

import org.junit.Assert.assertEquals
import org.junit.Test

class EtcdSectionsTest {
    @Test
    fun aHealthyClusterHasMembersOnly() {
        assertEquals(listOf(EtcdPart.MEMBERS to 0), etcdSections(EtcdListShape(statuses = 3)))
    }

    @Test
    fun theTopPanelsComeBeforeTheSnapshot() {
        val shape = EtcdListShape(error = true, defrag = true, memberPanel = true, snapshot = true, statuses = 3)
        assertEquals(listOf(EtcdPart.SNAPSHOT to 3, EtcdPart.MEMBERS to 4), etcdSections(shape))
    }

    @Test
    fun alarmsCountTheirRowsAndButtons() {
        // defrag 0, snapshot 1, alarms title 2, two alarm rows 3-4, fix 5, disarm 6, members 7.
        val shape = EtcdListShape(defrag = true, snapshot = true, alarms = 2, fixButton = true, disarmButton = true, statuses = 3)
        assertEquals(listOf(EtcdPart.SNAPSHOT to 1, EtcdPart.ALARMS to 2, EtcdPart.MEMBERS to 7), etcdSections(shape))
    }

    @Test
    fun aFailedAlarmCheckIsTheAlarmsSectionToo() {
        // The check's title and error (0-1), then the alarms found (2-3), one dot for both.
        val shape = EtcdListShape(alarmsError = true, alarms = 1)
        assertEquals(listOf(EtcdPart.ALARMS to 0, EtcdPart.MEMBERS to 4), etcdSections(shape))
    }

    @Test
    fun runningPanelsAndTheRecoverButtonShiftTheRest() {
        val shape = EtcdListShape(fixPanel = true, recoverPanel = true, recoverButton = true, disarmFailed = true)
        assertEquals(listOf(EtcdPart.FIX to 0, EtcdPart.RECOVER to 1, EtcdPart.MEMBERS to 4), etcdSections(shape))
    }

    @Test
    fun unprobedMembersFollowTheStatusesAndTheLagWarning() {
        // members title 0, lag warning 1, three statuses 2-4, unprobed title 5.
        val shape = EtcdListShape(lagging = true, statuses = 3, unprobed = 1)
        assertEquals(listOf(EtcdPart.MEMBERS to 0, EtcdPart.UNPROBED to 5), etcdSections(shape))
    }
}
