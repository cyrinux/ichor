package name.levis.ichor.model
import org.junit.Assert.*
import org.junit.Test

class LonghornSalvageTest {
    @Test fun selectionMustBeExplicitAndEligible() {
        val p = LonghornSalvagePreview(canSalvage = true, replicas = listOf(LonghornSalvageReplica(name = "r1", eligible = true), LonghornSalvageReplica(name = "r2")))
        assertFalse(p.accepts(emptySet()))
        assertTrue(p.accepts(setOf("r1")))
        assertFalse(p.accepts(setOf("r2")))
        assertFalse(p.accepts(setOf("foreign")))
        assertFalse(p.copy(error = "read denied").accepts(setOf("r1")))
        assertFalse(p.copy(canSalvage = false).accepts(setOf("r1")))
    }
    @Test fun blockedReasonDecodes() {
        val json = kotlinx.serialization.json.Json { ignoreUnknownKeys = true }
        val r = json.decodeFromString<LonghornSalvageReplica>("""{"name":"r1","node":"n1","eligible":false,"blocked":"node-not-ready"}""")
        assertEquals("node-not-ready", r.blocked)
        assertEquals("", json.decodeFromString<LonghornSalvageReplica>("""{"name":"r1"}""").blocked)
    }
}
