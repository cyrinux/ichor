package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import name.levis.ichor.monitor.dataIssuesOf
import org.junit.Assert.*
import org.junit.Test

class SnapshotsTest {
    @Test fun decodesClassesAndGatesDriverDefaults() {
        val report = TalosJson.decodeFromString(VolumeSnapshots.serializer(), """
            {"installed":true,"snapshots":[{"namespace":"shop","name":"nightly","class":"default"}],
             "classes":[{"name":"default","driver":"csi.example.io","default":true}]}
        """)
        assertEquals("default", report.snapshots.single().className)
        val pvc = StorageClaim(namespace="shop", name="orders", phase="Bound", provisioner="csi.example.io")
        assertTrue(report.canCreate(pvc))
        assertFalse(report.canCreate(pvc.copy(provisioner="other.example.io")))
        assertFalse(report.canCreate(pvc.copy(phase="Pending")))
        assertFalse(report.copy(classes=report.classes + report.classes).canCreate(pvc))
    }
    @Test fun alertsOnlyWatchedStaleClaimsAndUnsuspendedFailures() {
        val claims = listOf(SnapshotClaim(namespace="shop",name="orders",watched=true,stale=true),
            SnapshotClaim(namespace="shop",name="scratch",watched=false,stale=true))
        val jobs = listOf(BackupCronJob(namespace="shop",name="backup",failureStreak=3),
            BackupCronJob(namespace="shop",name="paused",failureStreak=3,suspended=true))
        val services = DataServices(snapshots=VolumeSnapshots(claims=claims), backupJobs=BackupJobStatus(jobs=jobs))
        assertEquals(mapOf("snapshots|shop/orders" to "warning", "backupjobs|shop/backup" to "critical"), dataIssuesOf(services))
        assertTrue(dataIssuesOf(services.copy(snapshots=services.snapshots!!.copy(error="Forbidden"),
            backupJobs=services.backupJobs!!.copy(jobsError="Forbidden"))).isEmpty())
    }
}
