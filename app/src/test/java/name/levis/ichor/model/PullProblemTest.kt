package name.levis.ichor.model

import name.levis.ichor.R
import name.levis.ichor.data.TalosJson
import name.levis.ichor.ui.checkup.checkupFix
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class PullProblemTest {
    private fun pod(json: String) = TalosJson.decodeFromString(KubePod.serializer(), json)

    @Test
    fun decodesThePullProblem() {
        val got = pod(
            """{"namespace":"web","name":"api-0","status":"ImagePullBackOff","waiting":"Back-off pulling image \"registry.example.test/api:1\"",
               "pull":{"kind":"unauthorized","image":"registry.example.test/api:1","registry":"registry.example.test","secrets":["regcred","backup"]}}""",
        )
        assertEquals("Back-off pulling image \"registry.example.test/api:1\"", got.waiting)
        assertEquals(PullProblem("unauthorized", "registry.example.test/api:1", "registry.example.test", listOf("regcred", "backup")), got.pull)
        assertEquals(PullKind.UNAUTHORIZED, got.pull?.pullKind)
    }

    @Test
    fun decodesAPodWithoutTheFields() {
        val got = pod("""{"namespace":"web","name":"api-0","status":"Running"}""")
        assertEquals("", got.waiting)
        assertNull(got.pull)
    }

    @Test
    fun decodesNullSecrets() {
        val got = pod("""{"namespace":"web","name":"api-0","pull":{"kind":"notFound","image":"app:1","registry":"docker.io","secrets":null}}""")
        assertEquals(emptyList<String>(), got.pull?.secrets)
    }

    @Test
    fun parsesEveryKind() {
        PullKind.entries.forEach { assertEquals(it, PullKind.parse(it.wire)) }
        assertEquals(PullKind.RATE_LIMITED, PullKind.parse("rateLimited"))
        assertEquals(PullKind.INVALID_NAME, PullKind.parse("invalidName"))
    }

    @Test
    fun anUnexpectedKindIsUnknown() {
        listOf("", "garbage", "NotFound", "RATELIMITED").forEach { assertEquals(it, PullKind.UNKNOWN, PullKind.parse(it)) }
        assertEquals(PullKind.UNKNOWN, PullProblem(kind = "somethingNew").pullKind)
    }

    @Test
    fun titles() {
        assertEquals(PullTitle(R.string.pod_pull_notFound, true), PullKind.NOT_FOUND.title())
        assertEquals(PullTitle(R.string.pod_pull_unauthorized, true), PullKind.UNAUTHORIZED.title())
        assertEquals(PullTitle(R.string.pod_pull_rateLimited, true), PullKind.RATE_LIMITED.title())
        assertEquals(PullTitle(R.string.pod_pull_tls, true), PullKind.TLS.title())
        assertEquals(PullTitle(R.string.pod_pull_network, true), PullKind.NETWORK.title())
        assertEquals(PullTitle(R.string.pod_pull_platform, false), PullKind.PLATFORM.title())
        assertEquals(PullTitle(R.string.pod_pull_invalidName, false), PullKind.INVALID_NAME.title())
        assertEquals(PullTitle(R.string.pod_pull_unknown, true), PullKind.UNKNOWN.title())
    }

    @Test
    fun hints() {
        assertEquals(R.string.pod_pull_hint_notFound, PullKind.NOT_FOUND.hint(hasSecrets = false))
        assertEquals(R.string.pod_pull_hint_rateLimited, PullKind.RATE_LIMITED.hint(hasSecrets = false))
        assertEquals(R.string.pod_pull_hint_tls, PullKind.TLS.hint(hasSecrets = false))
        assertEquals(R.string.pod_pull_hint_network, PullKind.NETWORK.hint(hasSecrets = false))
        assertEquals(R.string.pod_pull_hint_platform, PullKind.PLATFORM.hint(hasSecrets = false))
        assertEquals(R.string.pod_pull_hint_invalidName, PullKind.INVALID_NAME.hint(hasSecrets = false))
        assertEquals(R.string.pod_pull_hint_unknown, PullKind.UNKNOWN.hint(hasSecrets = true))
        assertEquals(R.string.pod_pull_hint_unauthorized, PullKind.UNAUTHORIZED.hint(hasSecrets = true))
        assertEquals(R.string.pod_pull_hint_unauthorized_none, PullKind.UNAUTHORIZED.hint(hasSecrets = false))
    }

    @Test
    fun theUnauthorizedHintListsTheSecrets() {
        val withSecrets = PullProblem("unauthorized", secrets = listOf("regcred", "backup"))
        assertEquals(R.string.pod_pull_hint_unauthorized, withSecrets.hint())
        assertEquals("regcred, backup", withSecrets.hintArg())
        val without = PullProblem("unauthorized")
        assertEquals(R.string.pod_pull_hint_unauthorized_none, without.hint())
        assertNull(without.hintArg())
        assertNull(PullProblem("notFound", secrets = listOf("regcred")).hintArg())
    }

    @Test
    fun checkupFixFollowsTheKind() {
        val fixes = mapOf(
            "notFound" to R.string.checkup_podImagePull_fix_notFound,
            "unauthorized" to R.string.checkup_podImagePull_fix_unauthorized,
            "rateLimited" to R.string.checkup_podImagePull_fix_rateLimited,
            "tls" to R.string.checkup_podImagePull_fix_tls,
            "network" to R.string.checkup_podImagePull_fix_network,
            "platform" to R.string.checkup_podImagePull_fix_platform,
            "invalidName" to R.string.checkup_podImagePull_fix_invalidName,
        )
        fixes.forEach { (reason, fix) -> assertEquals(reason, fix, checkupFix(CheckupKind.POD_IMAGE_PULL, reason)) }
        listOf("", "unknown", "ErrImagePull").forEach {
            assertEquals(it, R.string.checkup_podImagePull_fix, checkupFix(CheckupKind.POD_IMAGE_PULL, it))
        }
        assertEquals(R.string.checkup_podImagePull_fix, checkupFix(CheckupKind.POD_IMAGE_PULL))
        // Another kind keeps its fix whatever its reason.
        assertEquals(R.string.checkup_podCrashLoop_fix, checkupFix(CheckupKind.POD_CRASH_LOOP, "notFound"))
    }

    @Test
    fun snippetPrefillRoundTrips() {
        val prefill = SnippetPrefill("registry_mirror", mapOf("host" to "registry.example.test:5000", "endpoint" to "https://mirror.example.test/v2?a=b"))
        assertEquals(prefill, SnippetPrefill.decode(prefill.id, prefill.encodedParams()))
        assertEquals(SnippetPrefill("registry_mirror"), SnippetPrefill.decode("registry_mirror", ""))
        assertNull(SnippetPrefill.decode("", "host=x"))
        assertFalse(SnippetPrefill.decode("dns", "novalue")!!.params.containsKey("novalue"))
        assertTrue(SnippetPrefill.decode("dns", "a=b=c")!!.params["a"] == "b=c")
    }
}
