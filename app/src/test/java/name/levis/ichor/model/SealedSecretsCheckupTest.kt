package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import name.levis.ichor.ui.checkup.detail
import org.junit.Assert.assertEquals
import org.junit.Test

class SealedSecretsCheckupTest {
    // The demo checkup's Sealed Secrets findings (go/ichorgo/kube_checkup_demo.go).
    private val raw = """
        {"status":"critical","sections":[{"id":"secrets","status":"critical","checked":11,"findings":[
          {"kind":"sealedSecretNotSynced","severity":"critical","namespace":"shop","name":"legacy-cert","reason":"UnsealFailed","message":"failed to decrypt: no key could decrypt secret"},
          {"kind":"sealingKeyCompromised","severity":"warning","namespace":"kube-system","name":"sealed-secrets-key-rotated","reason":"keyCompromised"}]}]}
    """.trimIndent()

    @Test fun kindsKeepTheirWireNames() {
        val section = TalosJson.decodeFromString(CheckupReport.serializer(), raw).shownSections.single()
        assertEquals(listOf(CheckupKind.SEALED_SECRET_NOT_SYNCED, CheckupKind.SEALING_KEY_COMPROMISED), section.findings.map { it.kind })
        assertEquals("sealingKeyMissing", CheckupKind.SEALING_KEY_MISSING)
        assertEquals(setOf("sealedSecretNotSynced", "sealingKeyMissing"), DATA_SERVICE_ALERTED_KINDS)
    }

    @Test fun theUnsealFailureShowsItsReason() {
        val f = TalosJson.decodeFromString(CheckupReport.serializer(), raw).shownSections.single().findings[0]
        assertEquals("UnsealFailed: failed to decrypt: no key could decrypt secret", f.detail)
    }
}
