package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class SopsTest {
    @Test
    fun `decodes a file's metadata and shortens its keys`() {
        val json = """{"encrypted":true,"format":"yaml","recipients":[{"type":"age","id":"age1testrecipient00000000000000000000000000000000000000000000"},
            {"type":"kms","id":"arn:aws:kms:eu-west-1:000000000000:key/test-key-id-0000"}],"encryptedKeys":3,"clearKeys":["cluster.secret"]}"""
        val info = TalosJson.decodeFromString(SopsInfo.serializer(), json)
        assertTrue(info.encrypted)
        assertEquals(listOf("cluster.secret"), info.clearKeys)
        assertEquals("age1testre…000000", info.recipients[0].short)
        assertEquals("…key/test-key-id-0000", info.recipients[1].short)
    }

    @Test
    fun `ciphertext collapses and the secret marker is worded by the app`() {
        assertEquals("password: ENC[…]\nname: web", collapseSopsValues("password: ENC[AES256_GCM,data:abc=,iv:x,tag:y,type:str]\nname: web"))
        // An ENC[ in ordinary text does not swallow the lines after it.
        assertEquals("prefix = \"ENC[\"\nnext line ]", collapseSopsValues("prefix = \"ENC[\"\nnext line ]"))
        assertTrue(SopsWarning(warn = true, what = "Secret").isSecret)
        assertTrue(SopsWarning(warn = true, what = "TalosSecrets").isTalosBundle)
        assertEquals("cluster.secret", SopsWarning(warn = true, what = "cluster.secret").what)
    }
}
