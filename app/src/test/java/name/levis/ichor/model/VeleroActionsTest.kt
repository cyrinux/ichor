package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.*
import org.junit.Test

class VeleroActionsTest {
    @Test fun backupsDecodeWithDefaults() {
        val list = TalosJson.decodeFromString(VeleroBackups.serializer(), """
            {"namespace":"velero","schedule":"apps-daily","error":"","future":1,"backups":[
             {"name":"apps-daily-2","phase":"Completed","startedAt":10,"completedAt":20,"expiresAt":99,"finished":true,"totalItems":4},
             {"name":"apps-daily-1","phase":"Completed","startedAt":5,"finished":true,"deleting":true},
             {"name":"apps-daily-0","phase":"InProgress","startedAt":1,"running":true},
             {"name":"apps-daily-x","phase":"Failed","finished":true,"deleteErrors":["read-only location"]}]}
        """)
        assertEquals(listOf(true, false, false, true), list.backups.map { it.deletable })
        assertEquals(20L, list.backups[0].at)
        assertEquals(5L, list.backups[1].at)
        assertEquals(listOf("read-only location"), list.backups[3].deleteErrors)
        assertTrue(TalosJson.decodeFromString(VeleroBackupDetail.serializer(), "{}").includedNamespaces.isEmpty())
    }

    @Test fun backupNowIsBlockedByInvalidThenPausedThenRunning() {
        val s = VeleroSchedule(namespace = "velero", name = "apps-daily", phase = "Enabled")
        assertNull(s.backupNowBlocker)
        assertEquals(VeleroBackupBlocker.RUNNING, s.copy(inProgress = true).backupNowBlocker)
        assertEquals(VeleroBackupBlocker.PAUSED, s.copy(paused = true, inProgress = true).backupNowBlocker)
        assertEquals(VeleroBackupBlocker.INVALID, s.copy(phase = "FailedValidation", paused = true).backupNowBlocker)
    }
}
