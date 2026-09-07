package com.mycorrhizal.crm.e2e

import okhttp3.Cookie
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response
import org.json.JSONArray
import org.json.JSONObject
import java.net.URLEncoder
import java.util.concurrent.TimeUnit

/**
 * Minimal API harness for the E2E suite's *seeding*, *cleanup*, and *contract*
 * calls (issue #238). Deliberately independent of the app's Hilt graph and
 * [com.mycorrhizal.crm.network.ApiClient]: the tests drive the real UI, and
 * this helper only covers the setup/teardown and a few server-side checks the
 * UI cannot reach cheaply (idempotent user registration, per-test contact
 * creation/cleanup, audit-log reads and the audit-undo 400-on-delete check).
 *
 * Requests are plain blocking OkHttp calls on the instrumentation thread. The
 * login call captures the JWT from the Set-Cookie header exactly the way the
 * app's own ApiClient does — the backend mints the token in an httpOnly
 * cookie, never in the JSON body.
 */
class E2eBackend(
    private val serverUrl: String = E2eConfig.serverUrl,
) {
    /** A contact as created by this suite. [uid] is the vCard UID used as the
     *  audit log's entity_id. */
    data class SeedContact(val id: Long, val uid: String)

    private val apiBase = "$serverUrl/api/v1"
    private val jsonType = "application/json".toMediaType()
    private val client = OkHttpClient.Builder()
        .connectTimeout(15, TimeUnit.SECONDS)
        .readTimeout(30, TimeUnit.SECONDS)
        .writeTimeout(30, TimeUnit.SECONDS)
        .build()

    private var authToken: String? = null

    // --- session ------------------------------------------------------------

    /** A lightweight reachability probe (the unauthenticated liveness check).
     *  Used by tests that target a backend instance that only exists in some
     *  CI legs (issue #528's force-update backend) so they skip cleanly when
     *  it is not running. */
    fun isReachable(): Boolean =
        try {
            val response = client.newCall(
                Request.Builder().url("$serverUrl/health/live").build(),
            ).execute()
            response.use { it.isSuccessful }
        } catch (_: Exception) {
            false
        }

    /** Registers the seed account. 409 (already exists from a prior run) is
     *  treated as success. */
    fun registerSeedUser(
        username: String = E2eConfig.SEED_USERNAME,
        email: String = E2eConfig.SEED_EMAIL,
        password: String = E2eConfig.SEED_PASSWORD,
    ) {
        val body = JSONObject()
            .put("username", username)
            .put("email", email)
            .put("password", password)
            .toString()
        post("/register", body).use {
            check(it.code == 201 || it.code == 409) {
                "seed-user registration failed: ${it.code} ${it.body?.string().orEmpty()}"
            }
        }
    }

    /** Logs in as the seed user and keeps the bearer token for later calls. */
    fun login(
        identifier: String = E2eConfig.SEED_USERNAME,
        password: String = E2eConfig.SEED_PASSWORD,
    ): String {
        val body = JSONObject()
            .put("identifier", identifier)
            .put("password", password)
            .toString()
        post("/login", body).use {
            check(it.code == 200) { "seed-user login failed: ${it.code} ${it.body?.string().orEmpty()}" }
            val token = Cookie.parseAll("$serverUrl/".toHttpUrl(), it.headers)
                .firstOrNull { cookie -> cookie.name == "auth_token" }
                ?.value
            check(!token.isNullOrBlank()) { "login succeeded but no auth_token cookie was set" }
            authToken = token
            return token
        }
    }

    // --- contacts ------------------------------------------------------------

    /** Creates a contact whose display name is "$givenName $surname". */
    fun createContact(givenName: String, surname: String): SeedContact {
        val components = JSONArray()
            .put(JSONObject().put("kind", "given").put("value", givenName))
            .put(JSONObject().put("kind", "surname").put("value", surname))
        val body = JSONObject()
            .put("card", JSONObject().put("name", JSONObject().put("components", components)))
            .toString()
        post("/contacts", body, authenticated = true).use {
            check(it.code == 200 || it.code == 201) {
                "create contact failed: ${it.code} ${it.body?.string().orEmpty()}"
            }
            val json = JSONObject(it.body?.string().orEmpty())
            val contact = json.optJSONObject("contact") ?: json
            val id = contact.optLong("id")
            val uid = contact.optString("uid").takeIf { uid -> uid.isNotBlank() }
            check(id > 0 && uid != null) { "create contact response lacked id/uid: $json" }
            return SeedContact(id, uid)
        }
    }

    fun deleteContact(id: Long) {
        delete("/contacts/$id", authenticated = true).use {
            check(it.code in 200..299 || it.code == 404) {
                "delete contact failed: ${it.code} ${it.body?.string().orEmpty()}"
            }
        }
    }

    /** The detail endpoint's flat state, parsed as JSON — carries `archived`
     *  and `is_favorite` alongside the nested card. */
    fun contactState(id: Long): JSONObject {
        get("/contacts/$id", authenticated = true).use {
            check(it.code == 200) { "get contact failed: ${it.code} ${it.body?.string().orEmpty()}" }
            return JSONObject(it.body?.string().orEmpty())
        }
    }

    /** Deletes every contact whose firstname starts with
     *  [E2eConfig.TEST_CONTACT_PREFIX] — crashed-run leftovers. */
    fun cleanupTestContacts() {
        searchContacts(E2eConfig.TEST_CONTACT_PREFIX)
            .filter { (_, given) -> given.startsWith(E2eConfig.TEST_CONTACT_PREFIX) }
            .forEach { (id) -> deleteContact(id) }
    }

    /** Contacts matching [query], as (id, firstname, lastname). */
    private fun searchContacts(query: String): List<Triple<Long, String, String>> {
        val response = get("/contacts?search=${urlEncode(query)}&limit=200", authenticated = true)
        response.use {
            check(it.code == 200) { "contact search failed: ${it.code} ${it.body?.string().orEmpty()}" }
            val contacts = JSONObject(it.body?.string().orEmpty()).optJSONArray("contacts") ?: JSONArray()
            return (0 until contacts.length()).map { i ->
                val c = contacts.getJSONObject(i)
                Triple(c.optLong("id"), c.optString("firstname"), c.optString("lastname"))
            }
        }
    }

    // --- audit ---------------------------------------------------------------

    /** The caller's audit events, newest first, filtered when supplied. */
    fun auditEvents(entityType: String? = null, entityId: String? = null, limit: Int = 100): List<JSONObject> {
        val params = buildString {
            append("limit=").append(limit)
            entityType?.let { append("&entity_type=").append(urlEncode(it)) }
            entityId?.let { append("&entity_id=").append(urlEncode(it)) }
        }
        get("/audit?$params", authenticated = true).use {
            check(it.code == 200) { "audit list failed: ${it.code} ${it.body?.string().orEmpty()}" }
            val events = JSONObject(it.body?.string().orEmpty()).optJSONArray("audit_events") ?: JSONArray()
            return (0 until events.length()).map { events.getJSONObject(it) }
        }
    }

    /** POST /audit/:id/undo — returns the HTTP status (200 on success, 400 on
     *  a delete/non-contact event, 410 past retention). */
    fun undoAuditEvent(id: Long): Int =
        post("/audit/$id/undo", "", authenticated = true).use { it.code }

    // --- activities ----------------------------------------------------------

    /**
     * Every one of the caller's activities, walking the `next_cursor` pages to
     * exhaustion. ANDROID-02 (issue #479) uses these to count exactly how many
     * server Activities an outbox sync produced (matched by `external_ref` +
     * `date`), which is the "one record per queued entry" assertion.
     */
    fun listAllActivities(includeContacts: Boolean = false): List<JSONObject> {
        val activities = mutableListOf<JSONObject>()
        var cursor: String? = null
        repeat(MAX_ACTIVITY_PAGES) {
            val params = buildString {
                append("limit=100")
                cursor?.let { append("&cursor=").append(urlEncode(it)) }
                if (includeContacts) append("&include=contacts")
            }
            get("/activities?$params", authenticated = true).use { response ->
                check(response.code == 200) {
                    "activity list failed: ${response.code} ${response.body?.string().orEmpty()}"
                }
                val json = JSONObject(response.body?.string().orEmpty())
                val page = json.optJSONArray("activities") ?: JSONArray()
                for (i in 0 until page.length()) activities += page.getJSONObject(i)
                cursor = json.optString("next_cursor").takeIf { it.isNotBlank() }
            }
            if (cursor == null) return activities
        }
        error("activity list did not converge after $MAX_ACTIVITY_PAGES pages")
    }

    /** Deletes an activity by server id (used to clean up after an outbox test). */
    fun deleteActivity(id: Long) {
        delete("/activities/$id", authenticated = true).use {
            check(it.code in 200..299 || it.code == 404) {
                "delete activity failed: ${it.code} ${it.body?.string().orEmpty()}"
            }
        }
    }

    private companion object {
        /** A full offline-sync test run creates tens of activities, never thousands. */
        const val MAX_ACTIVITY_PAGES = 50
        /** The version the E2E harness advertises on its own API calls. */
        const val HARNESS_CLIENT_VERSION = "0.9.0"
    }

    // --- transport -----------------------------------------------------------

    private fun urlEncode(value: String): String = URLEncoder.encode(value, "UTF-8")

    // Issue #692: the harness acts as a current, supported client, so it
    // advertises a version at/above the compat backend's MIN_CLIENT_VERSION
    // floor (docker-compose.compat-test.yml sets 0.9.0). Without the header the
    // server would refuse the /register and /login seeding calls once a floor is
    // configured. The floor is only enforced on session-minting routes, so the
    // header is harmless against the no-floor suite backend.
    private fun Request.Builder.clientVersion() {
        header("X-Client-Version", HARNESS_CLIENT_VERSION)
    }

    private fun get(path: String, authenticated: Boolean = false): Response =
        client.newCall(
            Request.Builder().url(apiBase + path)
                .apply { clientVersion() }
                .apply { if (authenticated) auth() }
                .build(),
        ).execute()

    private fun post(path: String, body: String, authenticated: Boolean = false): Response =
        client.newCall(
            Request.Builder().url(apiBase + path)
                .apply { clientVersion() }
                .apply { if (authenticated) auth() }
                .post(body.toRequestBody(jsonType))
                .build(),
        ).execute()

    private fun delete(path: String, authenticated: Boolean = false): Response =
        client.newCall(
            Request.Builder().url(apiBase + path)
                .apply { clientVersion() }
                .apply { if (authenticated) auth() }
                .delete()
                .build(),
        ).execute()

    private fun Request.Builder.auth() {
        header("Authorization", "Bearer ${authToken.orEmpty()}")
    }
}
