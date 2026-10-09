package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class KubeStorageTest {

    private val json = """{"partialAccess":true,"claims":[
        {"namespace":"db","name":"data-pg-1","phase":"Bound","storageClass":"longhorn","provisioner":"driver.longhorn.io",
         "volume":"pv-pg","accessModes":["ReadWriteOnce"],"capacity":10737418240,"used":9663676416,"usedPercent":90,
         "measured":true,"pods":["pg-1"],"terminating":false,"managedBy":"cloudnative-pg","level":"warning"},
        {"namespace":"web","name":"uploads","phase":"Pending","accessModes":[],"capacity":0,"measured":false,"pods":[],"level":"newer-level"}]}"""

    private val storage = TalosJson.decodeFromString(KubeStorage.serializer(), json)

    @Test
    fun decodesTheCoreJson() {
        assertTrue(storage.partialAccess)
        val pg = storage.claims.first()
        assertEquals(0.9f, pg.usedFraction, 1e-6f)
        assertEquals(DataServiceKind.CNPG, pg.managedKind)
        assertEquals(StorageLevel.WARNING, pg.level)
        // A level this version does not know reads as ok, not as an error.
        assertEquals(StorageLevel.OK, storage.claims[1].level)
        assertNull(storage.claims[1].managedKind)
        assertEquals("db/data-pg-1", pg.key)
    }

    @Test
    fun searchesNameClassVolumeAndPods() {
        assertEquals(listOf("data-pg-1"), storage.claims.filteredClaims("PG-1").map { it.name })
        assertEquals(listOf("data-pg-1"), storage.claims.filteredClaims("longhorn").map { it.name })
        assertEquals(listOf("uploads"), storage.claims.filteredClaims("web/").map { it.name })
        assertEquals(2, storage.claims.filteredClaims("  ").size)
    }
}
