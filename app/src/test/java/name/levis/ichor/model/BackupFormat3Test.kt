package name.levis.ichor.model

import name.levis.ichor.data.AiSettings
import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class BackupFormat3Test {
    private val providers = listOf("anthropic", "openai-compatible")
    private val aiKey = "sk-test-not-a-real-key"
    private val gitToken = "glpat-test-not-a-real-token"
    private val nodePin = Pin(ShareTarget(target = ShareTarget.NODE, addr = "10.0.0.2"), "web-1")
    private val hosts = listOf(GitHost("git.example.org", GitHostKind.GITEA, listOf("ssh.git.example.org")))
    private val ai = AiSettings(
        enabled = true,
        provider = "anthropic",
        models = mapOf("anthropic" to "model-id"),
        baseUrls = mapOf("openai-compatible" to "https://llm.example.org/v1"),
        anonymize = false,
    )

    private fun payload(secrets: BackupSecrets?) = BackupPayload(
        platform = "android",
        talosconfig = "context: lab\n",
        settings = BackupSettings(
            allowScreenshots = true,
            appLockMode = BackupLockMode.BIOMETRIC,
            storageWarnPercent = 75,
            storageCriticalPercent = 88,
        ),
        pins = backupPins(listOf("aaaa", "bbbb")) { if (it == "aaaa") listOf(nodePin) else emptyList() },
        gitHosts = backupGitHosts(hosts),
        ai = backupAi(ai),
        secrets = secrets,
    )

    private fun roundTrip(payload: BackupPayload): Pair<String, BackupPayload> {
        val json = TalosJson.encodeToString(BackupPayload.serializer(), payload)
        return json to TalosJson.decodeFromString(BackupPayload.serializer(), json)
    }

    @Test
    fun roundTripWithSecrets() {
        val secrets = backupSecrets(mapOf("anthropic" to aiKey, "openai-compatible" to ""), mapOf("git.example.org" to gitToken))
        val (json, decoded) = roundTrip(payload(secrets))

        assertTrue("\"format\":3" in json)
        assertTrue("\"aliases\":[\"ssh.git.example.org\"]" in json)
        assertTrue("\"kind\":\"gitea\"" in json)
        assertEquals(mapOf("aaaa" to listOf(nodePin)), restoredPins(decoded.pins, listOf("aaaa")))
        assertEquals(hosts, restoredGitHosts(decoded.gitHosts))
        assertEquals(ai, restoredAi(decoded.ai, providers, AiSettings()))
        assertEquals(mapOf("anthropic" to aiKey), restoredAiKeys(decoded.secrets, providers))
        assertEquals(mapOf("git.example.org" to gitToken), restoredGitTokens(decoded.secrets, hosts))
        assertEquals(BackupLockMode.BIOMETRIC, decoded.settings.appLockMode)
        assertEquals(true, decoded.settings.allowScreenshots)
        assertEquals(75, decoded.settings.storageWarnPercent)
        assertEquals(88, decoded.settings.storageCriticalPercent)
    }

    @Test
    fun withoutTheOptInTheJsonHoldsNoKeyOrToken() {
        val (json, decoded) = roundTrip(payload(secrets = null))

        assertFalse("no secrets key at all", "\"secrets\"" in json)
        assertFalse(aiKey in json)
        assertFalse(gitToken in json)
        assertNull(decoded.secrets)
        assertEquals(emptyMap<String, String>(), restoredAiKeys(decoded.secrets, providers))
        assertEquals(emptyMap<String, String>(), restoredGitTokens(decoded.secrets, hosts))
        // The settings still travel.
        assertEquals(hosts, restoredGitHosts(decoded.gitHosts))
        assertEquals(ai, restoredAi(decoded.ai, providers, AiSettings()))
    }

    @Test
    fun noSecretsLeftIsNoSecretsKey() {
        assertNull(backupSecrets(mapOf("anthropic" to " "), emptyMap()))
        val onlyTokens = backupSecrets(emptyMap(), mapOf("git.example.org" to gitToken))
        val json = TalosJson.encodeToString(BackupSecrets.serializer(), onlyTokens!!)
        assertFalse("an empty map is left out", "aiKeys" in json)
    }

    @Test
    fun formatTwoPayloadStillRestores() {
        val json = """{"format":2,"platform":"ios","talosconfig":"","kubeconfig":"kind: Config",
            "settings":{"themeMode":"dark"},"clusters":{"aaaa":{"name":"Lab"}}}"""
        val payload = TalosJson.decodeFromString(BackupPayload.serializer(), json)

        assertEquals(2, payload.format)
        assertEquals("kind: Config", payload.kubeconfig)
        assertEquals(mapOf("aaaa" to "Lab"), restoredClusters(payload.clusters, listOf("aaaa")).names)
        assertNull("pins left as they are", restoredPins(payload.pins, listOf("aaaa")))
        assertEquals(emptyList<GitHost>(), restoredGitHosts(payload.gitHosts))
        assertNull("AI settings left as they are", restoredAi(payload.ai, providers, AiSettings()))
        assertNull(payload.settings.allowScreenshots)
        assertNull(payload.settings.appLockMode)
        assertNull(payload.settings.storageWarnPercent)
    }

    @Test
    fun restoredPinsReplaceEveryRestoredClusterAndFollowTheParseRules() {
        val clusterWide = Pin(ShareTarget(target = ShareTarget.CLUSTER), "all")
        val many = (1..Pins.MAX + 3).map { Pin(ShareTarget(target = ShareTarget.NODE, addr = "10.0.0.$it"), "n$it") }
        val saved = mapOf("aaaa" to many + clusterWide + many.first(), "gone" to listOf(nodePin))

        val restored = restoredPins(saved, listOf("aaaa", "bbbb"))!!

        assertEquals(setOf("aaaa", "bbbb"), restored.keys)
        assertEquals(many.take(Pins.MAX), restored.getValue("aaaa"))
        assertEquals("a cluster without pins in the backup has none", emptyList<Pin>(), restored.getValue("bbbb"))
    }

    @Test
    fun restoredAiDropsUnknownProviders() {
        val saved = BackupAi(
            enabled = true,
            provider = "future-provider",
            models = mapOf("future-provider" to "x", "anthropic" to " model-id "),
            baseUrls = mapOf("future-provider" to "https://llm.example.org"),
        )
        val current = AiSettings(provider = "openai-compatible")

        val restored = restoredAi(saved, providers, current)!!

        assertEquals("openai-compatible", restored.provider)
        assertEquals(mapOf("anthropic" to "model-id"), restored.models)
        assertEquals(emptyMap<String, String>(), restored.baseUrls)
        val keys = restoredAiKeys(BackupSecrets(aiKeys = mapOf("future-provider" to aiKey, "anthropic" to aiKey)), providers)
        assertEquals(mapOf("anthropic" to aiKey), keys)
    }

    @Test
    fun restoredGitHostsAreNormalisedAndTokensOnlyForThem() {
        val restored = restoredGitHosts(
            listOf(
                BackupGitHost("https://Git.Example.org/", "gitlab", listOf("git.example.org", "SSH.git.example.org")),
                BackupGitHost("git.example.org", "github"),
                BackupGitHost("  ", "gitea"),
                BackupGitHost("code.example.org", "newer-kind"),
            ),
        )

        assertEquals(
            listOf(
                GitHost("git.example.org", GitHostKind.GITLAB, listOf("ssh.git.example.org")),
                GitHost("code.example.org", GitHostKind.AUTO),
            ),
            restored,
        )
        val tokens = restoredGitTokens(BackupSecrets(gitHostTokens = mapOf("GIT.example.org" to gitToken, "other.example.org" to "t")), restored)
        assertEquals(mapOf("git.example.org" to gitToken), tokens)
    }

    @Test
    fun lockModeNamesNoSecret() {
        assertEquals("off", BackupLockMode.of(enabled = false, securityKeys = true))
        assertEquals("biometric", BackupLockMode.of(enabled = true, securityKeys = false))
        assertEquals("securityKey", BackupLockMode.of(enabled = true, securityKeys = true))
    }
}
