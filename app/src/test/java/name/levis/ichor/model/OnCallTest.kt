package name.levis.ichor.model

import kotlinx.serialization.builtins.MapSerializer
import kotlinx.serialization.builtins.serializer
import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class OnCallTest {
    @Test
    fun goGetsEveryFieldEvenTheDefaultOnes() {
        val json = OnCallConfig(token = "sample", serviceIds = listOf("P1")).toGoJson()
        assertTrue(json, "\"kind\":\"pagerduty\"" in json)
        assertTrue(json, "\"region\":\"us\"" in json)
        assertTrue(json, "\"token\":\"sample\"" in json)
    }

    @Test
    fun itIsSetWithATokenAndAService() {
        assertFalse(OnCallConfig().isSet)
        assertFalse(OnCallConfig(token = "sample").isSet)
        assertFalse(OnCallConfig(token = "  ", serviceIds = listOf("P1")).isSet)
        assertTrue(OnCallConfig(token = "sample", serviceIds = listOf("P1")).isSet)
    }

    @Test
    fun theIncidentOfAnAlertComesFromItsMatch() {
        val incidents = OnCallIncidents(
            incidents = listOf(OnCallIncident(id = "P1", status = OnCallStatus.TRIGGERED)),
            matches = mapOf("fa" to OnCallMatch("P1", OnCallStatus.TRIGGERED, OnCallConfidence.EXACT), "gone" to OnCallMatch("P2")),
        )
        assertEquals("P1", incidents.incidentOf("fa")?.id)
        assertNull(incidents.incidentOf("gone"))
        assertNull(incidents.incidentOf("other"))
    }

    @Test
    fun whoAcknowledgedFallsBackOnTheAssignee() {
        assertEquals("Alex", OnCallIncident(acknowledgedBy = "Alex", assignees = listOf("Sam")).acknowledgerName)
        assertEquals("Sam", OnCallIncident(assignees = listOf("Sam")).acknowledgerName)
        assertEquals("", OnCallIncident().acknowledgerName)
    }

    @Test
    fun aBackupOnlySaysAnIntegrationWasSet() {
        val clusters = backupClusters(listOf("fp"), emptyMap(), emptyMap(), emptySet(), emptyMap(), emptyMap(), oncallSet = setOf("fp"))
        assertTrue(clusters.getValue("fp").oncallSet)
        val json = TalosJson.encodeToString(MapSerializer(String.serializer(), BackupCluster.serializer()), clusters)
        assertFalse(json, "token" in json)
    }
}
