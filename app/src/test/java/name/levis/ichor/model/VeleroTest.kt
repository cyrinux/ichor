package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import name.levis.ichor.monitor.DATA_CRITICAL
import name.levis.ichor.monitor.DATA_WARNING
import name.levis.ichor.monitor.dataIssuesOf
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class VeleroTest {
    private val json = """
        {"velero":{"version":"v1","error":"",
         "schedules":[
          {"namespace":"velero","name":"failing","schedule":"0 3 * * *","paused":false,"phase":"Enabled","validationErrors":[],
           "health":"critical","reasons":["failed"],"storageLocation":"default","includedNamespaces":[],
           "lastBackup":{"name":"failing-1","phase":"Failed","startedAt":1000,"completedAt":2000,"errors":0,"warnings":0,"failureReason":"error getting backup store"},
           "lastSuccessAt":500,"inProgress":false},
          {"namespace":"velero","name":"partial","schedule":"@daily","phase":"Enabled","health":"warning","reasons":["partiallyFailed"],
           "storageLocation":"default","includedNamespaces":["app"],
           "lastBackup":{"name":"partial-1","phase":"PartiallyFailed","completedAt":2000,"errors":2,"warnings":3},"lastSuccessAt":1000,"inProgress":true},
          {"namespace":"velero","name":"invalid","schedule":"nope","phase":"FailedValidation","validationErrors":["invalid schedule"],
           "health":"warning","reasons":["invalid"],"lastBackup":null,"lastSuccessAt":0},
          {"namespace":"velero","name":"daily","schedule":"0 2 * * *","health":"ok","reasons":[],
           "lastBackup":{"name":"daily-1","phase":"Completed","completedAt":3000},"lastSuccessAt":3000},
          {"namespace":"velero","name":"paused","schedule":"0 2 * * *","paused":true,"health":"idle","reasons":[]}],
         "adhoc":[{"namespace":"velero","name":"before-upgrade","phase":"PartiallyFailed","completedAt":2500,"errors":1,"warnings":0,
           "failureReason":"","storageLocation":"default","health":"warning"}],
         "locations":[
          {"namespace":"velero","name":"offsite","provider":"aws","bucket":"offsite","default":false,"phase":"Unavailable","message":"rpc error","health":"critical"},
          {"namespace":"velero","name":"default","provider":"aws","bucket":"backups","default":true,"phase":"Available","health":"ok"}]}}
    """.trimIndent()

    private val services = TalosJson.decodeFromString(DataServices.serializer(), json)

    @Test
    fun decodesSchedulesBackupsAndLocations() {
        val v = services.velero!!
        assertEquals(5, v.schedules.size)
        assertEquals(listOf(VeleroReason.FAILED), v.schedules[0].reasonList)
        assertEquals("error getting backup store", v.schedules[0].lastBackup?.failureReason)
        assertEquals(2, v.schedules[1].lastBackup?.errors)
        assertTrue(v.schedules[1].inProgress)
        assertNull(v.schedules[2].lastBackup)
        assertEquals(ServiceHealth.IDLE, v.schedules[4].serviceHealth)
        assertEquals("velero/before-upgrade", v.adhoc.single().label)
        assertTrue(v.locations[1].default)
        assertEquals(listOf(DataServiceKind.VELERO), services.detected)
    }

    @Test
    fun summaryCountsSchedulesAndLooksAtLocationsAndAdhoc() {
        // 3 schedules, the unavailable location and the failed backup taken by hand need a look.
        assertEquals(ServiceSummary(total = 5, attention = 5, health = ServiceHealth.CRITICAL), services.summary(DataServiceKind.VELERO))
    }

    @Test
    fun alertsCoverSchedulesAndLocationsNotAdhoc() {
        assertEquals(
            mapOf(
                "velero|velero/failing" to DATA_CRITICAL,
                "velero|velero/invalid" to DATA_WARNING,
                "velero|BackupStorageLocation/velero/offsite" to DATA_CRITICAL,
                "velero|velero/partial" to DATA_WARNING,
            ),
            dataIssuesOf(services),
        )
    }

    @Test
    fun noLikelyCause() {
        assertEquals(emptyList<LikelyCause>(), services.likelyCauses(setOf("node-1")))
    }

    @Test
    fun hintsIncludeVelero() {
        val inventory = Inventory(apps = listOf(InventoryApp(id = "velero", name = "Velero"), InventoryApp(id = "longhorn", name = "Longhorn")))
        assertEquals("longhorn,velero", inventory.dataServiceHints())
    }
}
