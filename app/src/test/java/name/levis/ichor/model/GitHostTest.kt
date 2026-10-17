package name.levis.ichor.model

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class GitHostTest {

    @Test
    fun hostsAreKeyedWithoutSchemeSlashOrCase() {
        assertEquals("github.com", normalizeGitHost("https://github.com/"))
        assertEquals("github.com", normalizeGitHost("  GitHub.com  "))
        assertEquals("gitlab.example.org", normalizeGitHost("http://GitLab.Example.org//"))
        assertEquals("git.example.org:3000", normalizeGitHost("https://git.example.org:3000/api/v1/"))
        assertEquals("gitlab.example.org", normalizeGitHost("https://gitlab.example.org/group/repo.git"))
        assertEquals("git.example.org:3000", normalizeGitHost("http://git.example.org:3000/?tab=repos"))
        assertEquals("github.com", normalizeGitHost("github.com."))
        assertEquals("", normalizeGitHost("git example.org"))
        assertEquals("git.example.org", normalizeGitHost("https://user@git.example.org"))
        assertEquals("", normalizeGitHost(" https:// "))
    }

    @Test
    fun hostJsonCarriesKindAddressAndToken() {
        val json = parse(gitHostJson(GitHost("https://gitlab.example.org/", GitHostKind.GITLAB), " glpat-test ", demo = false))
        assertEquals(setOf("kind", "baseURL", "aliases", "token"), json.keys)
        assertEquals("gitlab", json.string("kind"))
        assertEquals("https://gitlab.example.org", json.string("baseURL"))
        assertEquals(JsonArray(emptyList()), json.getValue("aliases"))
        assertEquals("glpat-test", json.string("token"))
    }

    @Test
    fun hostJsonCarriesTheAliasesButOnlyTheBaseAddress() {
        val host = GitHost("git.example.org", GitHostKind.GITEA, listOf("ssh.git.example.org", "gitea-http.gitea.svc.cluster.local:3000"))
        val json = parse(gitHostJson(host, "t", demo = false))
        assertEquals("https://git.example.org", json.string("baseURL"))
        assertEquals(
            listOf("ssh.git.example.org", "gitea-http.gitea.svc.cluster.local:3000"),
            json.getValue("aliases").jsonArray.map { it.jsonPrimitive.content },
        )
    }

    @Test
    fun aliasesAreSplitNormalisedAndDeduplicated() {
        assertEquals(
            listOf("ssh.git.example.org", "gitea-http.gitea.svc.cluster.local:3000", "git.internal"),
            parseGitHostAliases(
                " SSH.git.example.org, https://gitea-http.gitea.svc.cluster.local:3000/org/repo.git\nssh.git.example.org ,,  git.internal  git.example.org",
                host = "https://git.example.org/",
            ),
        )
        assertEquals(emptyList<String>(), parseGitHostAliases("  , \n "))
        assertEquals(MAX_GIT_HOST_ALIASES, parseGitHostAliases((1..40).joinToString(",") { "h$it.example.org" }).size)
    }

    @Test
    fun aHostMatchesItsNameAndItsAliases() {
        val host = GitHost("git.example.org", aliases = listOf("ssh.git.example.org", "gitea-http.gitea.svc.cluster.local:3000"))
        assertTrue(host.matches("https://Git.example.org/"))
        assertTrue(host.matches("ssh.git.example.org"))
        // A source's host has no port (it comes from the web link): the alias with one matches.
        assertTrue(host.matches("gitea-http.gitea.svc.cluster.local"))
        assertFalse(host.matches("other.example.org"))
        assertEquals("ssh.git.example.org", normalizeGitHost("git@ssh.git.example.org:org/repo.git"))
        assertEquals("git.example.org:2222", normalizeGitHost("ssh://git@git.example.org:2222/org/repo.git"))
        assertEquals("git.example.org:3000", normalizeGitHost("git.example.org.:3000"))
        assertFalse(host.matches(""))
    }

    @Test
    fun aListSavedWithoutAliasesStillDecodes() {
        val hosts = Json { ignoreUnknownKeys = true }.decodeFromString(
            kotlinx.serialization.builtins.ListSerializer(GitHost.serializer()),
            """[{"host":"github.com","kind":"github"}]""",
        )
        assertEquals(listOf(GitHost("github.com", GitHostKind.GITHUB)), hosts)
        assertEquals(emptyList<String>(), hosts.single().aliases)
    }

    @Test
    fun autoIsLeftToTheGoCore() {
        assertEquals("auto", parse(gitHostJson(GitHost("github.com"), "t", demo = false)).string("kind"))
    }

    @Test
    fun aTokenWithQuotesIsEscaped() {
        assertEquals("a\"b\\c", parse(gitHostJson(GitHost("git.example.org", GitHostKind.GITEA), "a\"b\\c", demo = false)).string("token"))
    }

    @Test
    fun theDemoSendsNoTokenAndNoAddress() {
        assertEquals("""{"kind":"demo"}""", gitHostJson(GitHost("github.com"), "secret", demo = true))
    }

    @Test
    fun theHostNeverPrintsAToken() {
        assertEquals("GitHost(host=github.com, kind=GITHUB, aliases=[])", GitHost("github.com", GitHostKind.GITHUB).toString())
    }

    @Test
    fun kindsRoundTripTheirWireName() {
        GitHostKind.entries.forEach { assertEquals(it, GitHostKind.fromWire(it.wire)) }
        assertEquals(GitHostKind.AUTO, GitHostKind.fromWire("bitbucket"))
        assertFalse(GitHostKind.entries.any { it != GitHostKind.AUTO && it.label == null })
    }

    @Test
    fun aKindFromANewerAppReadsAsAuto() {
        // A list saved by a newer app with a kind this one does not know keeps its hosts.
        val hosts = Json.decodeFromString(
            kotlinx.serialization.builtins.ListSerializer(GitHost.serializer()),
            """[{"host":"git.example.org","kind":"bitbucket"},{"host":"github.com","kind":"github"}]""",
        )
        assertEquals(listOf(GitHost("git.example.org", GitHostKind.AUTO), GitHost("github.com", GitHostKind.GITHUB)), hosts)
        assertEquals(""""gitlab"""", Json.encodeToString(GitHostKindSerializer, GitHostKind.GITLAB))
    }

    private fun parse(json: String): JsonObject = Json.parseToJsonElement(json).jsonObject

    private fun JsonObject.string(key: String): String = getValue(key).jsonPrimitive.content
}
