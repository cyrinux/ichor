package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Test

class RunbookWaitStepTest {

    @Test
    fun decodesTheDetailAndTimeoutOfAWaitStep() {
        val json = """{"name":"Restart, then wait","steps":[
            {"id":"rolled-out","action":"wait","target":{"kind":"Deployment","namespace":"demo-shop","name":"ingest-api","node":""},
             "destructive":false,"continueOnError":false,"role":"os:admin","allowed":true,"denied":"",
             "detail":"wait up to 5 min for the rollout of ingest-api to finish","timeoutSeconds":300},
            {"id":"etcd","action":"assert","target":{"kind":"Cluster","namespace":"","name":"","node":""},
             "destructive":false,"continueOnError":false,"role":"os:reader","allowed":true,"denied":"","detail":"check that every etcd member is healthy","timeoutSeconds":0},
            {"id":"restart","action":"rolloutRestart","target":{"kind":"Deployment","namespace":"demo-shop","name":"ingest-api"},"destructive":true}],"errors":[]}"""
        val plan = TalosJson.decodeFromString(RunbookPlan.serializer(), json)
        val (wait, etcd, restart) = plan.steps
        assertEquals(300, wait.timeoutSeconds)
        assertEquals("wait up to 5 min for the rollout of ingest-api to finish", wait.detail)
        assertEquals("Cluster", etcd.target.label)
        assertEquals("Deployment demo-shop/ingest-api", restart.target.label)
        assertEquals("", restart.detail)
        assertEquals(0, restart.timeoutSeconds)
    }
}
