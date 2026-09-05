package com.mycorrhizal.crm.network

import com.mycorrhizal.crm.model.network.ActivityInput
import com.mycorrhizal.crm.model.network.CRMEnvelope
import com.mycorrhizal.crm.model.network.Card
import com.mycorrhizal.crm.model.network.ContactRecordInput
import com.mycorrhizal.crm.model.network.DeviceRegistrationInput
import com.mycorrhizal.crm.model.network.LoginResponse
import com.mycorrhizal.crm.model.network.Name
import com.mycorrhizal.crm.model.network.NoteInput
import com.mycorrhizal.crm.model.network.Reminder
import com.mycorrhizal.crm.model.network.ReminderRecurrence
import com.squareup.moshi.Moshi
import com.squareup.moshi.kotlin.reflect.KotlinJsonAdapterFactory
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import kotlinx.coroutines.runBlocking

class ApiClientTest {

    private lateinit var server: MockWebServer
    private lateinit var client: ApiClient

    @Before
    fun setup() {
        server = MockWebServer()
        server.start()
        val okHttp = OkHttpClient.Builder()
            .addInterceptor(
                BaseUrlInterceptor(BaseUrlProvider { server.url("/").toString().trimEnd('/') }),
            )
            .build()
        val moshi = Moshi.Builder()
            .add(KotlinJsonAdapterFactory())
            .build()
        client = ApiClient(okHttp, moshi)
    }

    @After
    fun teardown() {
        server.shutdown()
    }

    @Test
    fun `login captures the auth_token cookie and parses preferences`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setHeader("Set-Cookie", "auth_token=eyJhbGciOiJIUzI1NiJ9.abc; Path=/; HttpOnly")
                .setBody("""{"language":"en","date_format":"eu"}"""),
        )

        val result = client.login("alice", "secret")

        assertTrue(result.isSuccess)
        val login = result.getOrThrow()
        assertEquals("eyJhbGciOiJIUzI1NiJ9.abc", login.token)
        assertEquals("en", login.language)
        assertEquals("eu", login.dateFormat)

        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/login", request.path)
    }

    @Test
    fun `login returns null token when no auth cookie is set`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"language":"en","date_format":"eu"}"""),
        )

        val result = client.login("alice", "secret")

        assertTrue(result.isSuccess)
        assertEquals(null, result.getOrThrow().token)
    }

    @Test
    fun `login with bad credentials maps to Client 401`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(401)
                .setBody("""{"error":{"code":"invalid_credentials","message":"Invalid credentials"}}"""),
        )

        val result = client.login("alice", "wrong")

        assertTrue(result.isFailure)
        val error = result.exceptionOrNull() as ApiError
        assertTrue(error is ApiError.Client)
        assertEquals(401, (error as ApiError.Client).code)
    }

    // --- N8 two-factor login (issue #814) ---

    @Test
    fun `login on a 2fa account reports the challenge and captures the pending cookie`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setHeader("Set-Cookie", "2fa_pending=challenge-jwt.abc; Path=/; Max-Age=600; HttpOnly; SameSite=Strict")
                .setBody("""{"two_factor_required":true}"""),
        )

        val result = client.login("alice", "secret")

        assertTrue(result.isSuccess)
        val login = result.getOrThrow()
        assertTrue(login.twoFactorRequired)
        assertEquals("challenge-jwt.abc", login.pending2faCookie)
        assertEquals(null, login.token)
    }

    @Test
    fun `login on a 2fa account without a pending cookie reports it as null`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"two_factor_required":true}"""),
        )

        val result = client.login("alice", "secret")

        assertTrue(result.isSuccess)
        val login = result.getOrThrow()
        assertTrue(login.twoFactorRequired)
        assertEquals(null, login.pending2faCookie)
    }

    @Test
    fun `complete2faLogin forwards the pending cookie and captures the auth token`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setHeader("Set-Cookie", "auth_token=twofa-session-jwt; Path=/; HttpOnly")
                .setBody("""{"language":"de","date_format":"eu"}"""),
        )

        val result = client.complete2faLogin("123456", "challenge-jwt.abc")

        assertTrue(result.isSuccess)
        val login = result.getOrThrow()
        assertEquals("twofa-session-jwt", login.token)
        assertEquals("de", login.language)
        assertEquals("eu", login.dateFormat)

        val request = server.takeRequest()
        assertEquals("/api/v1/login/2fa", request.path)
        assertEquals("2fa_pending=challenge-jwt.abc", request.getHeader("Cookie"))
    }

    @Test
    fun `complete2faLogin maps a wrong code to Client 400`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(400)
                .setBody("""{"error":{"code":"invalid_code","message":"Invalid value for field 'code'"}}"""),
        )

        val result = client.complete2faLogin("000000", "challenge-jwt.abc")

        assertTrue(result.isFailure)
        val error = result.exceptionOrNull() as ApiError
        assertTrue(error is ApiError.Client)
        assertEquals(400, (error as ApiError.Client).code)
    }

    @Test
    fun `complete2faLogin maps an expired challenge to Client 401`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(401)
                .setBody("""{"error":{"code":"unauthorized","message":"Invalid or expired two-factor session. Please sign in again."}}"""),
        )

        val result = client.complete2faLogin("123456", "stale-challenge")

        assertTrue(result.isFailure)
        val error = result.exceptionOrNull() as ApiError
        assertTrue(error is ApiError.Client)
        assertEquals(401, (error as ApiError.Client).code)
    }

    // The lockout body is FLAT (`{error, message, retry_after, ...}`) with no
    // apperrors envelope — the message must be parsed and surfaced verbatim
    // (mirror the web's auth.ts 429 branch), not dumped as raw JSON.
    @Test
    fun `complete2faLogin surfaces the lockout message from a flat 429 body`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(429)
                .setBody(
                    """{"error":"Account temporarily locked","message":"Too many failed login attempts. Please try again later.","retry_after":300,"retry_after_at":"2026-09-05T12:00:00Z"}""",
                ),
        )

        val result = client.complete2faLogin("000000", "challenge-jwt.abc")

        assertTrue(result.isFailure)
        val error = result.exceptionOrNull() as ApiError
        assertTrue(error is ApiError.Client)
        assertEquals(429, (error as ApiError.Client).code)
        assertEquals("Too many failed login attempts. Please try again later.", error.message)
    }

    // --- N8 two-factor management (issue #814) ---

    @Test
    fun `two factor status parses enabled`() = runBlocking {
        server.enqueue(MockResponse().setResponseCode(200).setBody("""{"enabled":true}"""))

        val result = client.getTwoFactorStatus()

        assertTrue(result.isSuccess)
        assertTrue(result.getOrThrow().enabled)
    }

    @Test
    fun `two factor setup returns the pending secret and otpauth url`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"secret":"JBSWY3DPEHPK3PXP","otpauth_url":"otpauth://totp/Example:alice?secret=JBSWY3DPEHPK3PXP&issuer=Example"}"""),
        )

        val result = client.setupTwoFactor()

        assertTrue(result.isSuccess)
        val setup = result.getOrThrow()
        assertEquals("JBSWY3DPEHPK3PXP", setup.secret)
        assertEquals("otpauth://totp/Example:alice?secret=JBSWY3DPEHPK3PXP&issuer=Example", setup.otpauthUrl)

        val request = server.takeRequest()
        assertEquals("/api/v1/users/2fa/setup", request.path)
    }

    @Test
    fun `two factor confirm returns recovery codes and captures the reissued session token`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setHeader("Set-Cookie", "auth_token=reissued-jwt; Path=/; HttpOnly")
                .setBody(
                    """{"message":"Two-factor authentication enabled","recovery_codes":["ABCDE-FGHIJ-KLMNO","PQRST-UVWXY-ZABCD"]}""",
                ),
        )

        val result = client.confirmTwoFactor("123456")

        assertTrue(result.isSuccess)
        val body = result.getOrThrow()
        assertEquals(2, body.value.recoveryCodes.size)
        assertEquals("ABCDE-FGHIJ-KLMNO", body.value.recoveryCodes[0])
        assertEquals("reissued-jwt", body.reissuedToken)

        val request = server.takeRequest()
        assertEquals("/api/v1/users/2fa/confirm", request.path)
    }

    @Test
    fun `two factor disable returns the message and the reissued session token`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setHeader("Set-Cookie", "auth_token=reissued-jwt-2; Path=/; HttpOnly")
                .setBody("""{"message":"Two-factor authentication disabled"}"""),
        )

        val result = client.disableTwoFactor("654321")

        assertTrue(result.isSuccess)
        val body = result.getOrThrow()
        assertEquals("Two-factor authentication disabled", body.value.message)
        assertEquals("reissued-jwt-2", body.reissuedToken)
    }

    @Test
    fun `list contacts parses the cursor page`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """
                    {
                      "contacts": [
                        {"id": 1, "uid": "u1", "fn": "Alice Smith", "firstname": "Alice", "lastname": "Smith", "primary_email": "a@x.com"},
                        {"id": 2, "uid": "u2", "fn": "Bob Jones"}
                      ],
                      "next_cursor": "cursor-2",
                      "limit": 50,
                      "sync": {"mode": "incremental", "incremental": ["contacts"]}
                    }
                    """.trimIndent(),
                ),
        )

        val result = client.listContacts()

        assertTrue(result.isSuccess)
        val page = result.getOrThrow()
        assertEquals(2, page.contacts.size)
        assertEquals("Alice Smith", page.contacts[0].fn)
        assertEquals("cursor-2", page.nextCursor)
        assertEquals("incremental", page.sync?.mode)
    }

    @Test
    fun `list contacts sends query parameters`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody("""{"contacts":[],"next_cursor":""}"""),
        )

        client.listContacts(cursor = "c1", limit = 25, search = "ali")

        val request = server.takeRequest()
        assertEquals("/api/v1/contacts?cursor=c1&limit=25&search=ali", request.path)
    }

    @Test
    fun `list contacts sends the circle filter and archived toggle on the query string`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody("""{"contacts":[],"next_cursor":""}"""),
        )

        client.listContacts(includeArchived = true, circle = "Book club")

        val request = server.takeRequest()
        assertEquals("/api/v1/contacts?include_archived=true&circle=Book%20club", request.path)
    }

    @Test
    fun `list contacts sends the favorites filter on the query string`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody("""{"contacts":[],"next_cursor":""}"""),
        )

        client.listContacts(favorites = true)

        val request = server.takeRequest()
        assertEquals("/api/v1/contacts?favorites=true", request.path)
    }

    @Test
    fun `list contacts omits the favorites filter by default`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody("""{"contacts":[],"next_cursor":""}"""),
        )

        client.listContacts()

        val request = server.takeRequest()
        assertEquals("/api/v1/contacts", request.path)
    }

    @Test
    fun `list contacts parses is_favorite on summary rows`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody(
                """
                {
                  "contacts": [
                    {"id": 1, "uid": "u1", "fn": "Alice Smith", "firstname": "Alice", "is_favorite": true},
                    {"id": 2, "uid": "u2", "fn": "Bob Jones", "is_favorite": false},
                    {"id": 3, "uid": "u3", "fn": "Carol"}
                  ],
                  "next_cursor": ""
                }
                """.trimIndent(),
            ),
        )

        val result = client.listContacts()

        assertTrue(result.isSuccess)
        val page = result.getOrThrow()
        assertTrue(page.contacts[0].isFavorite)
        assertFalse(page.contacts[1].isFavorite)
        // Issue #212: is_favorite is always on the wire server-side, but an
        // absent key must still decode to the default false (never crash).
        assertFalse(page.contacts[2].isFavorite)
    }

    @Test
    fun `list contacts omits a blank circle filter`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody("""{"contacts":[],"next_cursor":""}"""),
        )

        client.listContacts(circle = "   ")

        val request = server.takeRequest()
        assertEquals("/api/v1/contacts", request.path)
    }

    @Test
    fun `list contacts with vcardUids sends repeatable vcard_uid params and skips pagination params`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody("""{"contacts":[],"next_cursor":""}"""),
        )

        client.listContacts(cursor = "c1", limit = 25, search = "ali", vcardUids = listOf("uid-1", "uid-2"))

        val request = server.takeRequest()
        assertEquals("/api/v1/contacts?vcard_uid=uid-1&vcard_uid=uid-2", request.path)
    }

    @Test
    fun `upload contact photo posts a multipart body with the photo field`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"ID": 9, "firstname": "Carol", "photo": "x_photo.jpg", "photo_thumbnail": "data:image/jpeg;base64,abc"}"""),
        )

        val result = client.uploadContactPhoto(9, "fake-jpeg-bytes".toByteArray(), "image/jpeg")

        assertTrue(result.isSuccess)

        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/contacts/9/profile_picture", request.path)
        assertTrue(
            "Expected multipart/form-data, got ${request.getHeader("Content-Type")}",
            request.getHeader("Content-Type").orEmpty().startsWith("multipart/form-data"),
        )
        val body = request.body.readUtf8()
        assertTrue("multipart must use the `photo` form field", body.contains("""name="photo""""))
        assertTrue("the image MIME type must ride along in the part", body.contains("image/jpeg"))
        assertTrue("the photo bytes must be the part's content", body.contains("fake-jpeg-bytes"))
    }

    @Test
    fun `upload contact photo maps the 10MB size rejection to a Client error`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(400)
                .setBody("""{"error":{"code":"validation_failed","message":"File too large. Maximum size is 10MB"}}"""),
        )

        val result = client.uploadContactPhoto(9, ByteArray(1024), "image/jpeg")

        assertTrue(result.isFailure)
        val error = result.exceptionOrNull() as ApiError
        assertTrue(error is ApiError.Client)
        assertEquals(400, (error as ApiError.Client).code)
    }

    @Test
    fun `list external identities sends the contact filter and parses the page`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """
                    {
                      "external_identities": [
                        {"id": "i1", "entity_id": "u5", "system": "paperless", "external_id": "doc-1", "sync_status": "synced"},
                        {"id": "i2", "entity_id": "u5", "system": "immich", "external_id": "person-1", "url": "https://immich.example/person"}
                      ],
                      "total": 2,
                      "next_cursor": "",
                      "limit": 100
                    }
                    """.trimIndent(),
                ),
        )

        val result = client.listExternalIdentities("u5")

        assertTrue(result.isSuccess)
        val page = result.getOrThrow()
        assertEquals(2, page.externalIdentities.size)
        assertEquals("paperless", page.externalIdentities[0].system)
        assertEquals("person-1", page.externalIdentities[1].externalId)
        assertEquals(2, page.total)

        val request = server.takeRequest()
        assertEquals("/api/v1/external-identities?contact_id=u5&limit=100", request.path)
    }

    @Test
    fun `delete external identity sends a DELETE`() = runBlocking {
        server.enqueue(MockResponse().setResponseCode(200).setBody("""{"message":"External identity deleted"}"""))

        val result = client.deleteExternalIdentity("i1")

        assertTrue(result.isSuccess)
        val request = server.takeRequest()
        assertEquals("DELETE", request.method)
        assertEquals("/api/v1/external-identities/i1", request.path)
    }

    @Test
    fun `immich config parses has_api_key`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"base_url": "https://immich.example", "has_api_key": true, "sync_enabled": true}"""),
        )

        val result = client.getImmichConfig()

        assertTrue(result.isSuccess)
        assertEquals(true, result.getOrThrow().hasApiKey)
        assertEquals("https://immich.example", result.getOrThrow().baseUrl)
    }

    @Test
    fun `immich people are unwrapped from the response`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"people": [{"id": "p1", "name": "Alice"}, {"id": "p2", "name": ""}]}"""),
        )

        val result = client.listImmichPeople()

        assertTrue(result.isSuccess)
        assertEquals(2, result.getOrThrow().size)
        assertEquals("Alice", result.getOrThrow()[0].name)

        val request = server.takeRequest()
        assertEquals("/api/v1/immich/people", request.path)
    }

    @Test
    fun `link immich contact posts to the contact link path`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(201)
                .setBody("""{"external_identity": {"id": "i1", "system": "immich"}}"""),
        )

        val result = client.linkImmichContact("u5", "p1", "Alice")

        assertTrue(result.isSuccess)
        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/immich/contacts/u5/link", request.path)
        val body = request.body.readUtf8()
        assertTrue(body.contains("\"person_id\":\"p1\""))
        assertTrue(body.contains("\"person_name\":\"Alice\""))
    }

    @Test
    fun `immich contact summary unwraps the nullable summary`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """
                    {"summary": {"identity": {"id": "i1"}, "person_name": "Alice", "photo_count": 7}}
                    """.trimIndent(),
                ),
        )

        val result = client.getImmichContactSummary("u5")

        assertTrue(result.isSuccess)
        assertEquals("Alice", result.getOrThrow()?.personName)
        assertEquals(7, result.getOrThrow()?.photoCount)
    }

    @Test
    fun `immich contact summary is null when unlinked`() = runBlocking {
        server.enqueue(MockResponse().setResponseCode(200).setBody("""{"summary": null}"""))

        val result = client.getImmichContactSummary("u5")

        assertTrue(result.isSuccess)
        assertEquals(null, result.getOrThrow())
    }

    @Test
    fun `immich contact assets are unwrapped from the response`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"assets": [{"id": "a1", "occurred_at": "2026-08-01T10:00:00Z"}]}"""),
        )

        val result = client.listImmichContactAssets("u5")

        assertTrue(result.isSuccess)
        assertEquals(1, result.getOrThrow().size)
        assertEquals("a1", result.getOrThrow()[0].id)

        val request = server.takeRequest()
        assertEquals("/api/v1/immich/contacts/u5/assets", request.path)
    }

    @Test
    fun `immich asset image returns the raw bytes`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setHeader("Content-Type", "image/jpeg")
                .setBody("fake-jpeg-image-bytes"),
        )

        val result = client.getImmichAssetImageBytes("u5", "a1")

        assertTrue(result.isSuccess)
        assertEquals("fake-jpeg-image-bytes", result.getOrThrow().toString(Charsets.UTF_8))

        val request = server.takeRequest()
        assertEquals("/api/v1/immich/contacts/u5/assets/a1/image", request.path)
    }

    // --- Issue #236: Immich config CRUD + test-connection ---

    @Test
    fun `save immich config puts to the config path`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"base_url": "https://immich.example", "has_api_key": true, "sync_enabled": true}"""),
        )

        val result = client.saveImmichConfig(
            com.mycorrhizal.crm.model.network.ImmichConfigInput(baseUrl = "https://immich.example", apiKey = "secret"),
        )

        assertTrue(result.isSuccess)
        val request = server.takeRequest()
        assertEquals("PUT", request.method)
        assertEquals("/api/v1/immich/config", request.path)
        assertTrue(request.body.readUtf8().contains("\"api_key\":\"secret\""))
    }

    @Test
    fun `delete immich config deletes the config path`() = runBlocking {
        server.enqueue(MockResponse().setResponseCode(200).setBody("""{"message": "Immich config deleted"}"""))

        val result = client.deleteImmichConfig()

        assertTrue(result.isSuccess)
        val request = server.takeRequest()
        assertEquals("DELETE", request.method)
        assertEquals("/api/v1/immich/config", request.path)
    }

    @Test
    fun `test immich connection reports a diagnosed failure as a success`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"ok": false, "stage": "auth", "message": "invalid API key"}"""),
        )

        val result = client.testImmichConnection()

        assertTrue(result.isSuccess)
        assertFalse(result.getOrThrow().ok)
        assertEquals("invalid API key", result.getOrThrow().message)
    }

    // --- Issue #236: Paperless create/link surface ---

    @Test
    fun `paperless config parses has_api_token`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"base_url": "https://paperless.example", "has_api_token": true}"""),
        )

        val result = client.getPaperlessConfig()

        assertTrue(result.isSuccess)
        assertTrue(result.getOrThrow().hasApiToken)
    }

    @Test
    fun `search paperless documents omits the query param when null`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"documents": [{"id": 1, "title": "Lease"}]}"""),
        )

        val result = client.searchPaperlessDocuments()

        assertTrue(result.isSuccess)
        assertEquals(1, result.getOrThrow().size)
        assertEquals("Lease", result.getOrThrow()[0].title)
        val request = server.takeRequest()
        assertEquals("/api/v1/paperless/documents", request.path)
    }

    @Test
    fun `search paperless documents includes the query param when present`() = runBlocking {
        server.enqueue(MockResponse().setResponseCode(200).setBody("""{"documents": []}"""))

        client.searchPaperlessDocuments("lease")

        val request = server.takeRequest()
        assertEquals("/api/v1/paperless/documents?query=lease", request.path)
    }

    @Test
    fun `link paperless contact posts the document id`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(201)
                .setBody("""{"external_identity": {"id": "i1", "system": "paperless"}}"""),
        )

        val result = client.linkPaperlessContact("u5", "42")

        assertTrue(result.isSuccess)
        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/paperless/contacts/u5/link", request.path)
        assertTrue(request.body.readUtf8().contains("\"document_id\":\"42\""))
    }

    // --- Issue #236: Seafile create/link surface ---

    @Test
    fun `seafile libraries are unwrapped from the response`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"libraries": [{"id": "r1", "name": "My Library", "type": "library"}]}"""),
        )

        val result = client.listSeafileLibraries()

        assertTrue(result.isSuccess)
        assertEquals(1, result.getOrThrow().size)
        assertEquals("My Library", result.getOrThrow()[0].name)
    }

    @Test
    fun `seafile dir listing sends repo id and path`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"items": [{"id": "f1", "name": "doc.pdf", "type": "file", "size": 1024}]}"""),
        )

        val result = client.listSeafileDir("r1", "/docs")

        assertTrue(result.isSuccess)
        assertEquals(1, result.getOrThrow().size)
        assertEquals(1024L, result.getOrThrow()[0].size)
        val request = server.takeRequest()
        assertEquals("/api/v1/seafile/libraries/r1/dir?path=%2Fdocs", request.path)
    }

    @Test
    fun `link seafile contact posts the item shape`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(201)
                .setBody("""{"external_identity": {"id": "i1", "system": "seafile"}}"""),
        )

        val result = client.linkSeafileContact(
            "u5",
            com.mycorrhizal.crm.model.network.SeafileLinkRequest(
                repoId = "r1",
                path = "/docs/doc.pdf",
                name = "doc.pdf",
                type = "file",
                size = 1024,
            ),
        )

        assertTrue(result.isSuccess)
        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/seafile/contacts/u5/link", request.path)
        val body = request.body.readUtf8()
        assertTrue(body.contains("\"repo_id\":\"r1\""))
        assertTrue(body.contains("\"path\":\"/docs/doc.pdf\""))
    }

    // --- Issue #236: Nextcloud create/link surface ---

    @Test
    fun `nextcloud config parses username and has_app_password`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"base_url": "https://nc.example", "username": "drew", "has_app_password": true}"""),
        )

        val result = client.getNextcloudConfig()

        assertTrue(result.isSuccess)
        assertEquals("drew", result.getOrThrow().username)
        assertTrue(result.getOrThrow().hasAppPassword)
    }

    @Test
    fun `nextcloud dir listing defaults to the dav root`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"items": [{"name": "Photos", "path": "/Photos", "type": "dir"}]}"""),
        )

        val result = client.listNextcloudDir()

        assertTrue(result.isSuccess)
        assertEquals(1, result.getOrThrow().size)
        assertEquals("Photos", result.getOrThrow()[0].name)
        val request = server.takeRequest()
        assertEquals("/api/v1/nextcloud/dir?path=%2F", request.path)
    }

    @Test
    fun `link nextcloud contact posts the item shape`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(201)
                .setBody("""{"external_identity": {"id": "i1", "system": "nextcloud"}}"""),
        )

        val result = client.linkNextcloudContact(
            "u5",
            com.mycorrhizal.crm.model.network.NextcloudLinkRequest(
                path = "/Photos/img.jpg",
                name = "img.jpg",
                type = "file",
                size = 2048,
            ),
        )

        assertTrue(result.isSuccess)
        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/nextcloud/contacts/u5/link", request.path)
        assertTrue(request.body.readUtf8().contains("\"path\":\"/Photos/img.jpg\""))
    }

    @Test
    fun `get contact parses the full record`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """
                    {
                      "id": 7,
                      "uid": "u7",
                      "etag": "abc",
                      "card": {
                        "name": {"full": "Carol King", "components": [{"kind": "given", "value": "Carol"}]},
                        "emails": [{"address": "carol@x.com", "contexts": ["work"]}],
                        "phones": [{"number": "+1-555-0100", "features": ["cell"]}]
                      },
                      "crm": {"circles": ["friends"]},
                      "archived": false
                    }
                    """.trimIndent(),
                ),
        )

        val result = client.getContact(7)

        assertTrue(result.isSuccess)
        val record = result.getOrThrow()
        assertEquals(7, record.id)
        assertEquals("Carol King", record.card?.name?.full)
        assertEquals("carol@x.com", record.card?.emails?.firstOrNull()?.address)
        assertEquals(listOf("friends"), record.crm?.circles)

        val request = server.takeRequest()
        assertEquals("/api/v1/contacts/7", request.path)
    }

    @Test
    fun `get briefing parses the full prep-view composite`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """
                    {
                      "contact_id": 7,
                      "uid": "u7",
                      "name": "Carol King",
                      "kind": "human",
                      "photo_thumbnail": "data:image/png;base64,abc",
                      "last_activity": {
                        "id": 41,
                        "title": "Coffee",
                        "type": "visit",
                        "description": "Catch-up",
                        "date": "2026-08-10T14:00:00Z"
                      },
                      "recent_notes": [
                        {"ID": 9, "content": "Talks about her garden"}
                      ],
                      "cadence": {
                        "policy": {
                          "id": "pol-1",
                          "entity_id": "u7",
                          "target_interval_days": 30,
                          "qualifying_types": ["visit", "meal"]
                        },
                        "health": {
                          "has_qualifying_interaction": true,
                          "last_interaction": "2026-08-10T14:00:00Z",
                          "next_due": "2026-09-09T14:00:00Z",
                          "overdue_by": 2
                        }
                      },
                      "open_agenda_items": [
                        {"id": "ag-1", "content": "Ask about the surgery"}
                      ],
                      "relationships": [
                        {
                          "edge": {
                            "id": "edge-1",
                            "source_id": "u7",
                            "target_id": "u8",
                            "type": "spouse_of",
                            "directional": false,
                            "status": "confirmed"
                          },
                          "other_party_contact_id": 8,
                          "other_party_name": "Bob Marley",
                          "other_party_uid": "u8",
                          "display_token": "spouse_of"
                        }
                      ],
                      "life_events": [
                        {"id": "le-1", "entity_id": "u7", "type": "graduated", "description": "MSc"}
                      ],
                      "upcoming_reminders": [
                        {"ID": 3, "message": "Send card", "remind_at": "2026-08-20T09:00:00Z"}
                      ],
                      "upcoming_dates": [
                        {"label": "birthday", "date": "--12-25", "days_until": 5}
                      ]
                    }
                    """.trimIndent(),
                ),
        )

        val result = client.getBriefing(7)

        assertTrue(result.isSuccess)
        val briefing = result.getOrThrow()
        assertEquals(7, briefing.contactId)
        assertEquals("Carol King", briefing.name)
        assertEquals("human", briefing.kind)
        assertEquals("data:image/png;base64,abc", briefing.photoThumbnail)

        assertEquals("Coffee", briefing.lastActivity?.title)
        assertEquals("visit", briefing.lastActivity?.type)
        assertEquals(1, briefing.recentNotes.size)
        assertEquals("Talks about her garden", briefing.recentNotes.first().content)

        assertEquals(30, briefing.cadence?.policy?.targetIntervalDays)
        assertEquals(listOf("visit", "meal"), briefing.cadence?.policy?.qualifyingTypes)
        assertTrue(briefing.cadence?.health?.hasQualifyingInteraction ?: false)
        assertEquals(2, briefing.cadence?.health?.overdueBy)

        assertEquals(1, briefing.openAgendaItems.size)
        assertEquals("Ask about the surgery", briefing.openAgendaItems.first().content)

        assertEquals(1, briefing.relationships.size)
        val rel = briefing.relationships.first()
        assertEquals("Bob Marley", rel.otherPartyName)
        assertEquals(8, rel.otherPartyContactId)
        assertEquals("spouse_of", rel.displayToken)
        assertEquals("spouse_of", rel.edge?.type)
        assertEquals("u8", rel.edge?.targetId)

        assertEquals(1, briefing.lifeEvents.size)
        assertEquals("graduated", briefing.lifeEvents.first().type)

        assertEquals(1, briefing.upcomingReminders.size)
        assertEquals("Send card", briefing.upcomingReminders.first().message)

        assertEquals(1, briefing.upcomingDates.size)
        assertEquals("birthday", briefing.upcomingDates.first().label)
        assertEquals(5, briefing.upcomingDates.first().daysUntil)

        val request = server.takeRequest()
        assertEquals("/api/v1/contacts/7/briefing", request.path)
    }

    @Test
    fun `get briefing tolerates every collection key being absent`() = runBlocking {
        // The exact web crash: a contact with no history once returned every
        // collection block ABSENT (not `[]`), and PrepViewPage dereferenced
        // `.length` on them — white-screening the page into the ErrorBoundary.
        // Decoding into the Kotlin model makes absent vs [] indistinguishable,
        // so assert against the raw JSON with the keys omitted, exactly the
        // trap /CLAUDE.md frontend trap #8 describes.
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """
                    {
                      "contact_id": 1,
                      "uid": "u1",
                      "name": "Empty"
                    }
                    """.trimIndent(),
                ),
        )

        val result = client.getBriefing(1)

        assertTrue(result.isSuccess)
        val briefing = result.getOrThrow()
        assertEquals("Empty", briefing.name)
        assertTrue(briefing.recentNotes.isEmpty())
        assertTrue(briefing.openAgendaItems.isEmpty())
        assertTrue(briefing.relationships.isEmpty())
        assertTrue(briefing.lifeEvents.isEmpty())
        assertTrue(briefing.upcomingReminders.isEmpty())
        assertTrue(briefing.upcomingDates.isEmpty())
        assertEquals(null, briefing.cadence)
        assertEquals(null, briefing.lastActivity)
    }

    @Test
    fun `get briefing normalizes an explicit JSON null collection to an empty list`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """
                    {
                      "contact_id": 2,
                      "uid": "u2",
                      "name": "Nulls",
                      "recent_notes": null,
                      "relationships": null,
                      "upcoming_dates": null
                    }
                    """.trimIndent(),
                ),
        )

        val result = client.getBriefing(2)

        assertTrue(result.isSuccess)
        val briefing = result.getOrThrow()
        assertTrue(briefing.recentNotes.isEmpty())
        assertTrue(briefing.relationships.isEmpty())
        assertTrue(briefing.upcomingDates.isEmpty())
    }

    @Test
    fun `get briefing failure maps to the backend error`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(404)
                .setBody("""{"error":{"code":"not_found","message":"Contact not found"}}"""),
        )

        val result = client.getBriefing(999)

        assertTrue(result.isFailure)
        val error = result.exceptionOrNull() as ApiError
        assertTrue(error is ApiError.Client)
        assertEquals(404, (error as ApiError.Client).code)
    }

    @Test
    fun `current user parses profile`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """{"id": 3, "username": "alice", "email": "a@x.com", "is_admin": true, "language": "de"}""",
                ),
        )

        val result = client.currentUser()

        assertTrue(result.isSuccess)
        val profile = result.getOrThrow()
        assertEquals(3, profile.id)
        assertEquals("alice", profile.username)
        assertTrue(profile.isAdmin)
        assertEquals("de", profile.language)
    }

    @Test
    fun `404 maps to Client error`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(404)
                .setBody("""{"error":{"code":"not_found","message":"Contact not found"}}"""),
        )

        val result = client.getContact(999)

        assertTrue(result.isFailure)
        val error = result.exceptionOrNull() as ApiError
        assertTrue(error is ApiError.Client)
        assertEquals(404, (error as ApiError.Client).code)
        assertEquals("Contact not found", error.body)
    }

    @Test
    fun `malformed json maps to Parse error`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody("""{"contacts": [}"""),
        )

        val result = client.listContacts()

        assertTrue(result.isFailure)
        val error = result.exceptionOrNull()
        assertNotNull(error)
    }

    @Test
    fun `create contact sends a POST body and unwraps the wrapped response`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(201)
                .setBody(
                    """
                    {
                      "message": "Contact created successfully",
                      "contact": {
                        "id": 9,
                        "uid": "u9",
                        "card": {"name": {"full": "Carol King"}},
                        "crm": {"circles": ["friends"]}
                      }
                    }
                    """.trimIndent(),
                ),
        )
        val input = ContactRecordInput(
            card = Card(name = Name(full = "Carol King")),
            crm = CRMEnvelope(circles = listOf("friends")),
        )

        val result = client.createContact(input)

        assertTrue(result.isSuccess)
        // §2.6: the POST wrapper is unwrapped; callers get the bare record.
        assertEquals(9, result.getOrThrow().id)
        assertEquals("Carol King", result.getOrThrow().card?.name?.full)

        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/contacts", request.path)
        // Body carries the neutral Card; check for the given-name component.
        assertTrue(request.body.readUtf8().contains("Carol"))
    }

    @Test
    fun `create contact with a 400 validation error maps to Client error`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(400)
                .setBody(
                    """{"error":{"code":"validation_failed","message":"at least one name component (kind=given) or name.full is required"}}""",
                ),
        )

        val result = client.createContact(ContactRecordInput(card = Card()))

        assertTrue(result.isFailure)
        val error = result.exceptionOrNull() as ApiError
        assertTrue(error is ApiError.Client)
        assertEquals(400, (error as ApiError.Client).code)
    }

    @Test
    fun `update contact sends a PUT to the contact path`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """
                    {
                      "id": 9,
                      "uid": "u9",
                      "card": {"name": {"full": "Carol King Renamed"}}
                    }
                    """.trimIndent(),
                ),
        )
        val input = ContactRecordInput(card = Card(name = Name(full = "Carol King Renamed")))

        val result = client.updateContact(9, input)

        assertTrue(result.isSuccess)
        assertEquals("Carol King Renamed", result.getOrThrow().card?.name?.full)

        val request = server.takeRequest()
        assertEquals("PUT", request.method)
        assertEquals("/api/v1/contacts/9", request.path)
    }

    @Test
    fun `list contact activities parses the bare activities array`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """
                    {
                      "activities": [
                        {"ID": 1, "title": "Coffee with Dana", "type": "visit", "date": "2026-08-01T10:00:00Z"},
                        {"ID": 2, "title": "Phone call", "type": "call", "date": "2026-08-02T11:00:00Z"}
                      ]
                    }
                    """.trimIndent(),
                ),
        )

        val result = client.listContactActivities(5)

        assertTrue(result.isSuccess)
        val activities = result.getOrThrow().activities
        assertEquals(2, activities.size)
        assertEquals(1, activities[0].id)
        assertEquals("Coffee with Dana", activities[0].title)
        assertEquals("call", activities[1].type)

        val request = server.takeRequest()
        assertEquals("/api/v1/contacts/5/activities", request.path)
    }

    @Test
    fun `list contact activities sends search date and cursor params and parses next_cursor`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """{"activities":[{"ID":1,"title":"Coffee with Dana"}],"next_cursor":"cursor-2","limit":25}""",
                ),
        )

        val result = client.listContactActivities(5, cursor = "c1", limit = 25, search = "coffee", fromDate = "2026-08-01", toDate = "2026-08-10")

        assertTrue(result.isSuccess)
        assertEquals("cursor-2", result.getOrThrow().nextCursor)

        val request = server.takeRequest()
        assertEquals(
            "/api/v1/contacts/5/activities?cursor=c1&limit=25&search=coffee&fromDate=2026-08-01&toDate=2026-08-10",
            request.path,
        )
    }

    @Test
    fun `delete activity sends a DELETE and succeeds`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody("""{"message":"Activity deleted"}"""),
        )

        val result = client.deleteActivity(7)

        assertTrue(result.isSuccess)
        val request = server.takeRequest()
        assertEquals("DELETE", request.method)
        assertEquals("/api/v1/activities/7", request.path)
    }

    @Test
    fun `delete activity surfaces a 404 as a failure`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(404).setBody("""{"error":"Activity not found"}"""),
        )

        val result = client.deleteActivity(999)

        assertTrue(result.isFailure)
    }

    @Test
    fun `create activity posts and unwraps the wrapped response`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """
                    {
                      "message": "Activity created successfully",
                      "activity": {"ID": 7, "title": "Lunch", "type": "meal"}
                    }
                    """.trimIndent(),
                ),
        )

        val result = client.createActivity(ActivityInput(title = "Lunch", type = "meal", contactIds = listOf(5)))

        assertTrue(result.isSuccess)
        assertEquals(7, result.getOrThrow().id)
        assertEquals("Lunch", result.getOrThrow().title)

        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/activities", request.path)
        assertTrue(request.body.readUtf8().contains("5"))
    }

    @Test
    fun `create activity sends the idempotency key header when one is supplied`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"message":"ok","activity":{"ID":7,"title":"Call"}}"""),
        )

        val result = client.createActivity(
            ActivityInput(title = "Call", date = "2026-09-04T00:00:00Z", type = "call"),
            idempotencyKey = "row-key-1",
        )

        assertTrue(result.isSuccess)
        val request = server.takeRequest()
        // ANDROID-02 (issue #479): the outbox's per-row retry key must reach the server on the
        // wire, or an ambiguous-failure retry creates a duplicate Activity (CON-04/ADR-0010).
        assertEquals("row-key-1", request.getHeader("Idempotency-Key"))
    }

    @Test
    fun `create activity sends no idempotency key header when none is supplied`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"message":"ok","activity":{"ID":7,"title":"Call"}}"""),
        )

        val result = client.createActivity(ActivityInput(title = "Call", type = "call"))

        assertTrue(result.isSuccess)
        val request = server.takeRequest()
        assertEquals(null, request.getHeader("Idempotency-Key"))
    }

    @Test
    fun `update activity sends a PUT and parses the raw activity`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """{"ID": 7, "title": "Lunch and coffee", "type": "meal"}""",
                ),
        )

        val result = client.updateActivity(7, ActivityInput(title = "Lunch and coffee"))

        assertTrue(result.isSuccess)
        assertEquals("Lunch and coffee", result.getOrThrow().title)

        val request = server.takeRequest()
        assertEquals("PUT", request.method)
        assertEquals("/api/v1/activities/7", request.path)
    }

    @Test
    fun `get activity parses a single activity with participants`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """
                    {
                      "ID": 7,
                      "title": "Lunch",
                      "type": "meal",
                      "contacts": [{"ID": 5, "firstname": "Dana"}, {"ID": 9, "firstname": "Carol"}],
                      "external_ref": "cal-event-42"
                    }
                    """.trimIndent(),
                ),
        )

        val result = client.getActivity(7)

        assertTrue(result.isSuccess)
        val activity = result.getOrThrow()
        assertEquals(7, activity.id)
        assertEquals(listOf(5, 9), activity.contacts?.mapNotNull { it.id.takeIf { id -> id != 0 } })
        assertEquals("cal-event-42", activity.externalRef)

        val request = server.takeRequest()
        assertEquals("/api/v1/activities/7", request.path)
    }

    @Test
    fun `list activities parses the cursor page`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """
                    {
                      "activities": [{"ID": 1, "title": "Visit"}],
                      "next_cursor": "cursor-9",
                      "limit": 25
                    }
                    """.trimIndent(),
                ),
        )

        val result = client.listActivities(cursor = "cursor-9", limit = 25)

        assertTrue(result.isSuccess)
        val page = result.getOrThrow()
        assertEquals(1, page.activities.size)
        assertEquals("cursor-9", page.nextCursor)
    }

    @Test
    fun `list activities with includeContacts appends the include query param`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200)
                .setBody("""{"activities": [], "next_cursor": "", "limit": 25}"""),
        )

        client.listActivities(includeContacts = true)

        val request = server.takeRequest()
        assertEquals("/api/v1/activities?include=contacts", request.path)
    }

    // Trap-#8 (`/CLAUDE.md`): GetActivities marshals a nil Go slice as JSON `null`, not `[]`, for
    // zero activities — a different failure mode than the absent-key one below. Both must parse.
    @Test
    fun `list activities normalizes an explicit JSON null to an empty list`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200)
                .setBody("""{"activities": null, "next_cursor": "", "limit": 25}"""),
        )

        val result = client.listActivities()

        assertTrue(result.isSuccess)
        assertEquals(0, result.getOrThrow().activities.size)
    }

    @Test
    fun `list activities tolerates the collection key being fully absent`() = runBlocking {
        server.enqueue(MockResponse().setResponseCode(200).setBody("""{"next_cursor": "", "limit": 25}"""))

        val result = client.listActivities()

        assertTrue(result.isSuccess)
        assertEquals(0, result.getOrThrow().activities.size)
    }

    // M9: the Notes drawer inbox — GET /notes, the N4 unfiled-notes queue (ticket's test case 3).
    @Test
    fun `list notes parses the cursor page and its total`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody(
                """
                {
                  "notes": [{"ID": 3, "content": "Buy milk", "date": "2026-08-01T10:00:00Z"}],
                  "next_cursor": "cursor-2",
                  "limit": 25,
                  "total": 9
                }
                """.trimIndent(),
            ),
        )

        val result = client.listNotes(cursor = "cursor-1", limit = 25)

        assertTrue(result.isSuccess)
        val page = result.getOrThrow()
        assertEquals(1, page.notes.size)
        assertEquals("Buy milk", page.notes[0].content)
        assertEquals("cursor-2", page.nextCursor)
        assertEquals(9, page.total)

        val request = server.takeRequest()
        assertEquals("/api/v1/notes?cursor=cursor-1&limit=25", request.path)
    }

    // Trap-#8 (`/CLAUDE.md`): GetUnassignedNotes marshals a nil Go slice as JSON `null`, not
    // `[]`, for zero unfiled notes — a different failure mode than the absent-key one below.
    @Test
    fun `list notes normalizes an explicit JSON null to an empty list`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200)
                .setBody("""{"notes": null, "next_cursor": "", "limit": 25, "total": 0}"""),
        )

        val result = client.listNotes()

        assertTrue(result.isSuccess)
        assertEquals(0, result.getOrThrow().notes.size)
    }

    @Test
    fun `list notes tolerates the collection key being fully absent`() = runBlocking {
        server.enqueue(MockResponse().setResponseCode(200).setBody("""{"total": 0}"""))

        val result = client.listNotes()

        assertTrue(result.isSuccess)
        assertEquals(0, result.getOrThrow().notes.size)
    }

    @Test
    fun `list contact notes parses the wrapped notes array`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """
                    {
                      "notes": [
                        {"ID": 3, "content": "Loves climbing", "date": "2026-08-01T10:00:00Z"},
                        {"ID": 4, "content": "Met at conference", "date": "2026-08-02T11:00:00Z"}
                      ]
                    }
                    """.trimIndent(),
                ),
        )

        val result = client.listContactNotes(5)

        assertTrue(result.isSuccess)
        val notes = result.getOrThrow().notes
        assertEquals(2, notes.size)
        assertEquals(3, notes[0].id)
        assertEquals("Loves climbing", notes[0].content)

        val request = server.takeRequest()
        assertEquals("/api/v1/contacts/5/notes", request.path)
    }

    @Test
    fun `list contact notes sends search date and cursor params and parses next_cursor`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """{"notes":[{"ID":3,"content":"Loves climbing"}],"next_cursor":"cursor-2","limit":25}""",
                ),
        )

        val result = client.listContactNotes(5, cursor = "c1", limit = 25, search = "climb", fromDate = "2026-08-01", toDate = "2026-08-10")

        assertTrue(result.isSuccess)
        assertEquals("cursor-2", result.getOrThrow().nextCursor)

        val request = server.takeRequest()
        assertEquals(
            "/api/v1/contacts/5/notes?cursor=c1&limit=25&search=climb&fromDate=2026-08-01&toDate=2026-08-10",
            request.path,
        )
    }

    @Test
    fun `list contact notes omits blank filters`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody("""{"notes":[],"next_cursor":""}"""),
        )

        client.listContactNotes(5, search = "   ")

        val request = server.takeRequest()
        assertEquals("/api/v1/contacts/5/notes", request.path)
    }

    @Test
    fun `delete note sends a DELETE and succeeds`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody("""{"message":"Note deleted"}"""),
        )

        val result = client.deleteNote(3)

        assertTrue(result.isSuccess)
        val request = server.takeRequest()
        assertEquals("DELETE", request.method)
        assertEquals("/api/v1/notes/3", request.path)
    }

    @Test
    fun `delete note surfaces a 404 as a failure`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(404).setBody("""{"error":"Note not found"}"""),
        )

        val result = client.deleteNote(999)

        assertTrue(result.isFailure)
    }

    @Test
    fun `create note posts to the contact path and unwraps`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """
                    {
                      "message": "Note created successfully",
                      "note": {"ID": 3, "content": "Loves climbing"}
                    }
                    """.trimIndent(),
                ),
        )

        val result = client.createNote(5, NoteInput(content = "Loves climbing"))

        assertTrue(result.isSuccess)
        assertEquals(3, result.getOrThrow().id)
        assertEquals("Loves climbing", result.getOrThrow().content)

        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/contacts/5/notes", request.path)
    }

    @Test
    fun `create unassigned note posts to the notes path`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """{"message": "Note created successfully", "note": {"ID": 3, "content": "Loves climbing"}}""",
                ),
        )

        val result = client.createUnassignedNote(NoteInput(content = "Loves climbing"))

        assertTrue(result.isSuccess)
        assertEquals(3, result.getOrThrow().id)

        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/notes", request.path)
    }

    @Test
    fun `update note sends a PUT and unwraps the wrapped response`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """{"message": "Note updated successfully", "note": {"ID": 3, "content": "Loves rock climbing"}}""",
                ),
        )

        val result = client.updateNote(3, NoteInput(content = "Loves rock climbing"))

        assertTrue(result.isSuccess)
        assertEquals(3, result.getOrThrow().id)
        assertEquals("Loves rock climbing", result.getOrThrow().content)

        val request = server.takeRequest()
        assertEquals("PUT", request.method)
        assertEquals("/api/v1/notes/3", request.path)
    }

    @Test
    fun `get note parses a single note`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """{"ID": 3, "content": "Loves climbing", "date": "2026-08-01T10:00:00Z"}""",
                ),
        )

        val result = client.getNote(3)

        assertTrue(result.isSuccess)
        assertEquals("Loves climbing", result.getOrThrow().content)
    }

    @Test
    fun `list contact reminders parses the wrapped reminders array`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """
                    {
                      "reminders": [
                        {"ID": 1, "message": "Call Dana", "remind_at": "2026-08-10T14:00:00Z", "recurrence": "weekly"},
                        {"ID": 2, "message": "Birthday gift", "remind_at": "2026-08-15T09:00:00Z", "recurrence": "yearly"}
                      ]
                    }
                    """.trimIndent(),
                ),
        )

        val result = client.listContactReminders(5)

        assertTrue(result.isSuccess)
        val reminders = result.getOrThrow().reminders
        assertEquals(2, reminders.size)
        assertEquals("Call Dana", reminders[0].message)
        assertEquals("weekly", reminders[0].recurrence)

        val request = server.takeRequest()
        assertEquals("/api/v1/contacts/5/reminders", request.path)
    }

    @Test
    fun `create reminder posts and unwraps the wrapped response`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """
                    {
                      "message": "Reminder created successfully",
                      "reminder": {"ID": 1, "message": "Call Dana", "recurrence": "once"}
                    }
                    """.trimIndent(),
                ),
        )
        val reminder = Reminder(
            message = "Call Dana",
            remindAt = "2026-08-10T14:00:00Z",
            recurrence = ReminderRecurrence.ONCE,
            contactId = 5,
        )

        val result = client.createReminder(5, reminder)

        assertTrue(result.isSuccess)
        assertEquals(1, result.getOrThrow().id)

        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/contacts/5/reminders", request.path)
    }

    @Test
    fun `complete reminder parses the complete response`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """{"message": "Reminder completed", "reminder": {"ID": 1, "message": "Call Dana", "completed": false}}""",
                ),
        )

        val result = client.completeReminder(1)

        assertTrue(result.isSuccess)
        assertEquals(1, result.getOrThrow().reminder?.id)

        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/reminders/1/complete", request.path)
    }

    @Test
    fun `complete reminder with skip true appends the skip query param`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"message": "Reminder skipped", "reminder": {"ID": 1, "message": "Call Dana", "completed": false}}"""),
        )

        val result = client.completeReminder(1, skip = true)

        assertTrue(result.isSuccess)
        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/reminders/1/complete?skip=true", request.path)
    }

    @Test
    fun `get dashboard parses all four widgets from one composite response`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """
                    {
                      "birthdays": [
                        {"type": "contact", "name": "Alice", "birthday": "--12-25", "photo_thumbnail": "data:image/png;base64,abc", "contact_id": 1}
                      ],
                      "random_contacts": [
                        {"ID": 3, "firstname": "Bob", "lastname": "Smith", "nickname": "Bobby", "photo_thumbnail": "data:image/png;base64,def"}
                      ],
                      "upcoming_reminders": [
                        {"ID": 7, "message": "Call Dana", "remind_at": "2026-08-15T09:00:00Z", "recurrence": "weekly", "by_mail": true, "contact_id": 3, "contact_name": "Bobby Smith"}
                      ],
                      "overdue": [
                        {"policy": {"id": "cadence-1", "entity_id": "u3", "target_interval_days": 30}, "health": {"has_qualifying_interaction": true, "next_due": "2026-08-01T00:00:00Z", "overdue_by": 13}, "contact_id": 3, "contact_name": "Bobby Smith", "photo_thumbnail": "data:image/png;base64,ghi"}
                      ],
                      "reach_out_suggestions": [
                        {"id": "s1", "contact_vcard_uid": "u3", "kind": "organization", "old_value": "OldCo", "new_value": "NewCo", "audit_event_id": 42, "reminder_id": 9, "status": "pending", "contact_id": 3, "contact_name": "Bobby Smith", "photo_thumbnail": "data:image/png;base64,jkl"}
                      ]
                    }
                    """.trimIndent(),
                ),
        )

        val result = client.getDashboard()

        assertTrue(result.isSuccess)
        val dashboard = result.getOrThrow()
        // Birthdays.
        assertEquals(1, dashboard.birthdays.size)
        assertEquals("Alice", dashboard.birthdays[0].name)
        assertEquals(1L, dashboard.birthdays[0].contactId)
        // Random contacts — id reads the wire's PascalCase `ID` (the random
        // block is ContactResponse/gorm.Model, not the list's slim
        // ContactSummary), so it must NOT default to 0 here.
        assertEquals(1, dashboard.randomContacts.size)
        assertEquals("Bobby", dashboard.randomContacts[0].nickname)
        assertEquals(3, dashboard.randomContacts[0].id)
        // Upcoming reminders — the M3 embedded contact name must survive parsing.
        assertEquals(1, dashboard.upcomingReminders.size)
        assertEquals(7, dashboard.upcomingReminders[0].id)
        assertEquals("Call Dana", dashboard.upcomingReminders[0].message)
        assertEquals("Bobby Smith", dashboard.upcomingReminders[0].contactName)
        assertEquals("weekly", dashboard.upcomingReminders[0].recurrence)
        assertEquals(true, dashboard.upcomingReminders[0].byMail)
        assertEquals(3, dashboard.upcomingReminders[0].contactId)
        // Overdue cadences.
        assertEquals(1, dashboard.overdue.size)
        assertEquals("cadence-1", dashboard.overdue[0].policy?.id)
        assertEquals(13, dashboard.overdue[0].health?.overdueBy)
        // Issue #177: reach-out suggestions.
        assertEquals(1, dashboard.reachOutSuggestions.size)
        assertEquals("s1", dashboard.reachOutSuggestions[0].id)
        assertEquals("organization", dashboard.reachOutSuggestions[0].kind)
        assertEquals("OldCo", dashboard.reachOutSuggestions[0].oldValue)
        assertEquals("NewCo", dashboard.reachOutSuggestions[0].newValue)
        assertEquals(3L, dashboard.reachOutSuggestions[0].contactId)
        assertEquals("Bobby Smith", dashboard.reachOutSuggestions[0].contactName)

        val request = server.takeRequest()
        assertEquals("GET", request.method)
        assertEquals("/api/v1/dashboard", request.path)
    }

    @Test
    fun `get dashboard normalizes empty arrays to empty lists`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """
                    {
                      "birthdays": [],
                      "random_contacts": [],
                      "upcoming_reminders": [],
                      "overdue": [],
                      "favorites": [],
                      "reach_out_suggestions": []
                    }
                    """.trimIndent(),
                ),
        )

        val result = client.getDashboard()

        assertTrue(result.isSuccess)
        val dashboard = result.getOrThrow()
        assertTrue(dashboard.birthdays.isEmpty())
        assertTrue(dashboard.randomContacts.isEmpty())
        assertTrue(dashboard.upcomingReminders.isEmpty())
        assertTrue(dashboard.overdue.isEmpty())
        assertTrue(dashboard.favorites.isEmpty())
        assertTrue(dashboard.reachOutSuggestions.isEmpty())
    }

    @Test
    fun `get dashboard tolerates every collection key being absent`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{}"""),
        )

        val result = client.getDashboard()

        assertTrue(result.isSuccess)
        val dashboard = result.getOrThrow()
        assertTrue(dashboard.birthdays.isEmpty())
        assertTrue(dashboard.randomContacts.isEmpty())
        assertTrue(dashboard.upcomingReminders.isEmpty())
        assertTrue(dashboard.overdue.isEmpty())
        assertTrue(dashboard.favorites.isEmpty())
        assertTrue(dashboard.reachOutSuggestions.isEmpty())
    }

    @Test
    fun `dismiss reach-out suggestion posts to the suggestion-specific endpoint`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"message": "Suggestion dismissed"}"""),
        )

        val result = client.dismissReachOutSuggestion("suggestion-1")

        assertTrue(result.isSuccess)
        assertEquals("Suggestion dismissed", result.getOrThrow().message)

        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/reach-out-suggestions/suggestion-1/dismiss", request.path)
    }

    @Test
    fun `get dashboard parses the favorites block with PascalCase ID`() = runBlocking {
        // Issue #212: the favorites block is a list of ContactResponse (gorm.Model),
        // exactly like random_contacts — the id reads the wire's PascalCase `ID`.
        server.enqueue(
            MockResponse().setResponseCode(200).setBody(
                """
                {
                  "birthdays": [],
                  "random_contacts": [],
                  "upcoming_reminders": [],
                  "overdue": [],
                  "favorites": [
                    {"ID": 9, "firstname": "Zebra", "lastname": "Smith", "nickname": "Z", "photo_thumbnail": "data:image/png;base64,abc"},
                    {"ID": 2, "firstname": "Alice", "lastname": "Jones"}
                  ]
                }
                """.trimIndent(),
            ),
        )

        val result = client.getDashboard()

        assertTrue(result.isSuccess)
        val favorites = result.getOrThrow().favorites
        assertEquals(2, favorites.size)
        assertEquals(9, favorites[0].id)
        assertEquals("Zebra", favorites[0].firstname)
        assertEquals("Z", favorites[0].nickname)
        assertEquals(2, favorites[1].id)
    }

    @Test
    fun `update reminder sends a PUT and unwraps the wrapped response`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """{"message": "Reminder updated successfully", "reminder": {"ID": 1, "message": "Call Dana weekly", "recurrence": "weekly"}}""",
                ),
        )

        val result = client.updateReminder(
            1,
            Reminder(message = "Call Dana weekly", recurrence = ReminderRecurrence.WEEKLY),
        )

        assertTrue(result.isSuccess)
        assertEquals(1, result.getOrThrow().id)
        assertEquals("Call Dana weekly", result.getOrThrow().message)

        val request = server.takeRequest()
        assertEquals("PUT", request.method)
        assertEquals("/api/v1/reminders/1", request.path)
    }

    @Test
    fun `get reminder parses a single reminder`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """{"ID": 1, "message": "Call Dana", "recurrence": "weekly"}""",
                ),
        )

        val result = client.getReminder(1)

        assertTrue(result.isSuccess)
        assertEquals("Call Dana", result.getOrThrow().message)
    }

    @Test
    fun `delete reminder sends a DELETE and succeeds`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody("""{"message":"Reminder deleted"}"""),
        )

        val result = client.deleteReminder(7)

        assertTrue(result.isSuccess)
        val request = server.takeRequest()
        assertEquals("DELETE", request.method)
        assertEquals("/api/v1/reminders/7", request.path)
    }

    @Test
    fun `delete reminder surfaces a 404 as a failure`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(404).setBody("""{"error":"Reminder not found"}"""),
        )

        val result = client.deleteReminder(999)

        assertTrue(result.isFailure)
    }

    @Test
    fun `list contact reminder completions parses the wrapped completions array`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody(
                """
                {
                  "completions": [
                    {"ID": 1, "contact_id": 5, "message": "Call Dana", "completed_at": "2026-08-12T10:00:00Z"},
                    {"ID": 2, "reminder_id": 3, "contact_id": 5, "message": "Gift", "completed_at": "2026-08-11T09:00:00Z"}
                  ]
                }
                """.trimIndent(),
            ),
        )

        val result = client.listContactReminderCompletions(5)

        assertTrue(result.isSuccess)
        val completions = result.getOrThrow().completions
        assertEquals(2, completions.size)
        assertEquals("Call Dana", completions[0].message)
        assertEquals(1, completions[0].id)
        assertEquals(3, completions[1].reminderId)

        val request = server.takeRequest()
        assertEquals("GET", request.method)
        assertEquals("/api/v1/contacts/5/reminder-completions", request.path)
    }

    @Test
    fun `list contact reminder completions normalizes an empty completions array`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody("""{"completions": []}"""),
        )

        val result = client.listContactReminderCompletions(5)

        assertTrue(result.isSuccess)
        assertTrue(result.getOrThrow().completions.isEmpty())
    }

    @Test
    fun `list contact reminder completions normalizes an explicit JSON null to an empty list`() = runBlocking {
        // The backend's GetCompletionsForContact uses a nil Go slice when a contact has no
        // completions, which gin.H serializes as JSON null, not []. Must not crash (the
        // /CLAUDE.md trap #8 family — a collection field that's absent OR null).
        server.enqueue(
            MockResponse().setResponseCode(200).setBody("""{"completions": null}"""),
        )

        val result = client.listContactReminderCompletions(5)

        assertTrue(result.isSuccess)
        assertTrue(result.getOrThrow().completions.isEmpty())
    }

    @Test
    fun `delete reminder completion sends a DELETE and succeeds`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody("""{"message":"Reminder completion deleted"}"""),
        )

        val result = client.deleteReminderCompletion(7)

        assertTrue(result.isSuccess)
        val request = server.takeRequest()
        assertEquals("DELETE", request.method)
        assertEquals("/api/v1/reminder-completions/7", request.path)
    }

    @Test
    fun `delete reminder completion surfaces a 404 as a failure`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(404).setBody("""{"error":"Reminder completion not found"}"""),
        )

        val result = client.deleteReminderCompletion(999)

        assertTrue(result.isFailure)
    }

    @Test
    fun `search parses notes and activities with snippets and resolved_relation`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """
                    {
                      "query": "mom",
                      "resolved_relation": "parent_of",
                      "contacts": [{"id": 99, "fn": "Should Be Ignored"}],
                      "notes": [
                        {"ID": 1, "content": "called mom", "date": "2026-01-01", "contact_id": 5, "contact_name": "Dana White", "snippet": "called <b>mom</b>"}
                      ],
                      "activities": [
                        {"ID": 2, "title": "Coffee with mom", "date": "2026-01-02", "snippet": "Coffee with <b>mom</b>"}
                      ]
                    }
                    """.trimIndent(),
                ),
        )

        val result = client.search("mom", limit = 5)

        assertTrue(result.isSuccess)
        val search = result.getOrThrow()
        assertEquals("parent_of", search.resolvedRelation)
        assertEquals(1, search.notes.size)
        assertEquals("Dana White", search.notes[0].contactName)
        assertEquals("called <b>mom</b>", search.notes[0].snippet)
        assertEquals(1, search.activities.size)
        assertEquals("Coffee with mom", search.activities[0].title)

        val request = server.takeRequest()
        assertEquals("GET", request.method)
        assertEquals("/api/v1/search?q=mom&limit=5", request.path)
    }

    @Test
    fun `search response's contacts group never reaches the parsed model`() = runBlocking {
        // T87: the contact list is the sole authority for contact results; SearchResult has no
        // `contacts` property to leak them into, regardless of what the server sends.
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"query": "ali", "contacts": [{"id": 1, "fn": "Alice"}], "notes": [], "activities": []}"""),
        )

        val result = client.search("ali")

        assertTrue(result.isSuccess)
        assertEquals(0, result.getOrThrow().notes.size)
        assertEquals(0, result.getOrThrow().activities.size)
    }

    @Test
    fun `search omits optional params when not provided`() = runBlocking {
        server.enqueue(MockResponse().setResponseCode(200).setBody("""{"notes": [], "activities": []}"""))

        client.search("al")

        val request = server.takeRequest()
        assertEquals("/api/v1/search?q=al", request.path)
    }

    @Test
    fun `listFieldDefinitions parses the definitions list`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody(
                """
                {
                  "field_definitions": [
                    {"id": "d1", "label": "Coffee order", "key": "coffee_order", "target": "contact",
                     "type": "string", "projection": "internal-only", "sensitivity": "normal",
                     "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z"}
                  ],
                  "total": 1, "next_cursor": "", "limit": 100
                }
                """.trimIndent(),
            ),
        )

        val result = client.listFieldDefinitions()

        assertTrue(result.isSuccess)
        val defs = result.getOrThrow().definitions
        assertEquals(1, defs.size)
        assertEquals("Coffee order", defs[0].label)
        assertEquals("string", defs[0].type)

        val request = server.takeRequest()
        assertEquals("/api/v1/field-definitions", request.path)
    }

    @Test
    fun `listFieldDefinitions normalizes an explicit JSON null to an empty list`() = runBlocking {
        // T84: gin.H always emits the key, but a nil Go slice serializes as JSON null, not [] —
        // a different failure mode than /CLAUDE.md trap #8's absent-key one. Both must not crash.
        server.enqueue(
            MockResponse().setResponseCode(200)
                .setBody("""{"field_definitions": null, "total": 0, "next_cursor": "", "limit": 100}"""),
        )

        val result = client.listFieldDefinitions()

        assertTrue(result.isSuccess)
        assertEquals(0, result.getOrThrow().definitions.size)
    }

    @Test
    fun `listFieldDefinitions tolerates the collection key being fully absent`() = runBlocking {
        server.enqueue(MockResponse().setResponseCode(200).setBody("""{"total": 0}"""))

        val result = client.listFieldDefinitions()

        assertTrue(result.isSuccess)
        assertEquals(0, result.getOrThrow().definitions.size)
    }

    @Test
    fun `listContactFieldValues parses the values list`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody(
                """{"field_values": [{"id": 1, "field_definition_id": "d1", "entity_id": "u1", "value": "Latte"}]}""",
            ),
        )

        val result = client.listContactFieldValues(5)

        assertTrue(result.isSuccess)
        val values = result.getOrThrow().values
        assertEquals(1, values.size)
        assertEquals("Latte", values[0].value)

        val request = server.takeRequest()
        assertEquals("GET", request.method)
        assertEquals("/api/v1/contacts/5/field-values", request.path)
    }

    @Test
    fun `a value written via replaceContactFieldValues round-trips through a subsequent read`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody(
                """{"message": "Field values saved successfully", "field_values": [{"id": 1, "field_definition_id": "d1", "entity_id": "u1", "value": "Latte"}]}""",
            ),
        )
        server.enqueue(
            MockResponse().setResponseCode(200).setBody(
                """{"field_values": [{"id": 1, "field_definition_id": "d1", "entity_id": "u1", "value": "Latte"}]}""",
            ),
        )

        val writeResult = client.replaceContactFieldValues(
            5,
            com.mycorrhizal.crm.model.network.ContactFieldValuesInput(
                fieldValues = listOf(
                    com.mycorrhizal.crm.model.network.FieldValueInput(fieldDefinitionId = "d1", value = "Latte"),
                ),
            ),
        )
        val readResult = client.listContactFieldValues(5)

        assertTrue(writeResult.isSuccess)
        assertTrue(readResult.isSuccess)
        assertEquals("Latte", readResult.getOrThrow().values.first().value)

        val putRequest = server.takeRequest()
        assertEquals("PUT", putRequest.method)
        assertEquals("/api/v1/contacts/5/field-values", putRequest.path)
        assertTrue(putRequest.body.readUtf8().contains("\"field_definition_id\":\"d1\""))
    }

    @Test
    fun `update relationship edge sends a PUT and parses the raw edge`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """{"id": "e1", "source_id": "u1", "target_id": "u2", "type": "spouse_of", "sensitivity": "private"}""",
                ),
        )

        val result = client.updateRelationshipEdge(
            "e1",
            com.mycorrhizal.crm.model.network.RelationshipEdgeInput(
                sourceId = "u1",
                targetId = "u2",
                type = "spouse_of",
                sensitivity = "private",
            ),
        )

        assertTrue(result.isSuccess)
        assertEquals("spouse_of", result.getOrThrow().type)
        assertEquals("private", result.getOrThrow().sensitivity)

        val request = server.takeRequest()
        assertEquals("PUT", request.method)
        assertEquals("/api/v1/relationship-edges/e1", request.path)
    }

    // --- M24: contact-level actions (delete/archive/unarchive/export) ---

    @Test
    fun `delete contact sends a DELETE to the contact route`() = runBlocking {
        server.enqueue(MockResponse().setResponseCode(200).setBody("""{"message":"deleted"}"""))

        val result = client.deleteContact(5)

        assertTrue(result.isSuccess)
        val request = server.takeRequest()
        assertEquals("DELETE", request.method)
        assertEquals("/api/v1/contacts/5", request.path)
    }

    @Test
    fun `delete contact failure maps to the backend error`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(404).setBody("""{"error":{"code":"not_found","message":"Contact not found"}}"""),
        )

        val result = client.deleteContact(5)

        assertTrue(result.isFailure)
        val error = result.exceptionOrNull() as ApiError
        assertTrue(error is ApiError.Client)
        assertEquals(404, (error as ApiError.Client).code)
    }

    @Test
    fun `archive contact posts to the archive route`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"id":5,"archived":true,"firstname":"Dana","lastname":"White"}"""),
        )

        val result = client.archiveContact(5)

        assertTrue(result.isSuccess)
        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/contacts/5/archive", request.path)
    }

    @Test
    fun `unarchive contact posts to the unarchive route`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"id":5,"archived":false,"firstname":"Dana","lastname":"White"}"""),
        )

        val result = client.unarchiveContact(5)

        assertTrue(result.isSuccess)
        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/contacts/5/unarchive", request.path)
    }

    // --- Issue #212: favorite/unfavorite (web #173) ---

    @Test
    fun `favorite contact posts to the favorite route`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"ID":5,"is_favorite":true,"firstname":"Dana","lastname":"White"}"""),
        )

        val result = client.favoriteContact(5)

        assertTrue(result.isSuccess)
        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/contacts/5/favorite", request.path)
    }

    @Test
    fun `unfavorite contact posts to the unfavorite route`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"ID":5,"is_favorite":false,"firstname":"Dana","lastname":"White"}"""),
        )

        val result = client.unfavoriteContact(5)

        assertTrue(result.isSuccess)
        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/contacts/5/unfavorite", request.path)
    }

    @Test
    fun `favorite contact failure maps to the backend error`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(404).setBody("""{"error":{"code":"not_found","message":"Contact not found"}}"""),
        )

        val result = client.favoriteContact(999)

        assertTrue(result.isFailure)
        val error = result.exceptionOrNull() as ApiError
        assertTrue(error is ApiError.Client)
        assertEquals(404, (error as ApiError.Client).code)
    }

    @Test
    fun `export single contact vcard 4 returns the raw file bytes`() = runBlocking {
        val vcf = "BEGIN:VCARD\r\nVERSION:4.0\r\nFN:Dana White\r\nUID:u1\r\nEND:VCARD\r\n"
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setHeader("Content-Type", "text/vcard; charset=utf-8")
                .setBody(vcf),
        )

        val result = client.exportContactVcf("u1")

        assertTrue(result.isSuccess)
        assertEquals(vcf.toByteArray().contentToString(), result.getOrThrow().contentToString())
        val request = server.takeRequest()
        assertEquals("GET", request.method)
        assertEquals("/api/v1/export/vcf?vcard_uid=u1", request.path)
    }

    @Test
    fun `export single contact vcard 3 adds the version param`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setHeader("Content-Type", "text/vcard; charset=utf-8")
                .setBody("BEGIN:VCARD\r\nVERSION:3.0\r\nEND:VCARD\r\n"),
        )

        val result = client.exportContactVcf("u1", version = 3)

        assertTrue(result.isSuccess)
        val request = server.takeRequest()
        assertEquals("/api/v1/export/vcf?vcard_uid=u1&version=3", request.path)
    }

    @Test
    fun `export failure parses the JSON error instead of returning bytes`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(400).setBody("""{"error":{"code":"validation","message":"bad request"}}"""),
        )

        val result = client.exportContactVcf("missing")

        assertTrue(result.isFailure)
        val error = result.exceptionOrNull() as ApiError
        assertTrue(error is ApiError.Client)
        assertEquals(400, (error as ApiError.Client).code)
    }

    // M9 item 4: confirmVcfImport had zero callers/tests before this ticket wired a VCF import
    // screen to it — same request/response shape as the (also-untested) CSV confirmImport.
    @Test
    fun `confirmVcfImport posts to the vcf confirm path and parses the result`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"total_processed": 3, "created": 2, "updated": 1, "skipped": 0, "errors": []}"""),
        )

        val result = client.confirmVcfImport(
            com.mycorrhizal.crm.model.network.ImportConfirmRequest(
                sessionId = "session-1",
                actions = listOf(
                    com.mycorrhizal.crm.model.network.RowImportAction(rowIndex = 0, action = "add"),
                    com.mycorrhizal.crm.model.network.RowImportAction(rowIndex = 1, action = "update"),
                ),
            ),
        )

        assertTrue(result.isSuccess)
        assertEquals(2, result.getOrThrow().created)
        assertEquals(1, result.getOrThrow().updated)

        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/contacts/import/vcf/confirm", request.path)
        val body = request.body.readUtf8()
        assertTrue(body.contains("session-1"))
        assertTrue(body.contains("\"action\":\"update\""))
    }

    // --- M12: cadence policies (5 new client methods) ---

    @Test
    fun `list cadence policies sends the entity_id filter and parses the wrapped list`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """{"cadence_policies": [
                        {"id": "p1", "entity_id": "u1", "target_interval_days": 30,
                         "qualifying_types": ["call"], "health": {"has_qualifying_interaction": true, "overdue_by": 2}}
                    ], "total": 1, "next_cursor": "", "limit": 25}""",
                ),
        )

        val result = client.listCadencePolicies("u1")

        assertTrue(result.isSuccess)
        val page = result.getOrThrow()
        assertEquals(1, page.cadencePolicies.size)
        assertEquals(30, page.cadencePolicies[0].targetIntervalDays)
        assertEquals(listOf("call"), page.cadencePolicies[0].qualifyingTypes)
        assertEquals(2, page.cadencePolicies[0].health?.overdueBy)

        val request = server.takeRequest()
        assertEquals("/api/v1/cadence-policies?entity_id=u1", request.path)
    }

    @Test
    fun `list cadence policies normalizes an absent policy array to empty`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody("""{"total": 0, "next_cursor": "", "limit": 25}"""),
        )

        val result = client.listCadencePolicies("u1")

        assertTrue(result.isSuccess)
        assertTrue(result.getOrThrow().cadencePolicies.isEmpty())
    }

    @Test
    fun `get cadence policy parses the raw policy with health`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """{"id": "p1", "entity_id": "u1", "target_interval_days": 30,
                        "health": {"has_qualifying_interaction": false, "overdue_by": 0}}""",
                ),
        )

        val result = client.getCadencePolicy("p1")

        assertTrue(result.isSuccess)
        assertEquals("p1", result.getOrThrow().id)
        assertTrue(result.getOrThrow().qualifyingTypes.isEmpty())
        assertTrue(result.getOrThrow().health?.hasQualifyingInteraction == false)

        val request = server.takeRequest()
        assertEquals("/api/v1/cadence-policies/p1", request.path)
    }

    @Test
    fun `create cadence policy posts the input and unwraps the created policy`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(201)
                .setBody(
                    """{"message": "Cadence policy created", "cadence_policy":
                        {"id": "p1", "entity_id": "u1", "target_interval_days": 14, "qualifying_types": []}}""",
                ),
        )

        val result = client.createCadencePolicy(
            com.mycorrhizal.crm.model.network.CadencePolicyInput(entityId = "u1", targetIntervalDays = 14),
        )

        assertTrue(result.isSuccess)
        assertEquals("p1", result.getOrThrow().id)
        assertEquals(14, result.getOrThrow().targetIntervalDays)

        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/cadence-policies", request.path)
        val body = request.body.readUtf8()
        assertTrue(body.contains("\"entity_id\":\"u1\""))
        assertTrue(body.contains("\"target_interval_days\":14"))
        // An empty selection is sent as an empty array, never defaulted away.
        assertTrue(body.contains("\"qualifying_types\":[]"))
    }

    @Test
    fun `update cadence policy sends a PUT and parses the raw policy`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """{"id": "p1", "entity_id": "u1", "target_interval_days": 90,
                        "qualifying_types": ["visit"], "health": {"has_qualifying_interaction": true, "overdue_by": 0}}""",
                ),
        )

        val result = client.updateCadencePolicy(
            "p1",
            com.mycorrhizal.crm.model.network.CadencePolicyInput(
                entityId = "u1",
                targetIntervalDays = 90,
                qualifyingTypes = listOf("visit"),
            ),
        )

        assertTrue(result.isSuccess)
        assertEquals(90, result.getOrThrow().targetIntervalDays)
        assertEquals(listOf("visit"), result.getOrThrow().qualifyingTypes)

        val request = server.takeRequest()
        assertEquals("PUT", request.method)
        assertEquals("/api/v1/cadence-policies/p1", request.path)
        assertTrue(request.body.readUtf8().contains("\"qualifying_types\":[\"visit\"]"))
    }

    @Test
    fun `delete cadence policy sends a DELETE to the policy route`() = runBlocking {
        server.enqueue(MockResponse().setResponseCode(200).setBody("""{"message": "Cadence policy deleted"}"""))

        val result = client.deleteCadencePolicy("p1")

        assertTrue(result.isSuccess)
        val request = server.takeRequest()
        assertEquals("DELETE", request.method)
        assertEquals("/api/v1/cadence-policies/p1", request.path)
    }


    // --- M15: contact sharing (7 new client methods) ---

    @Test
    fun `list incoming contact shares parses the cursor page with usernames`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """
                    {
                      "contact_shares": [
                        {"id": "s1", "from_user_id": 7, "to_user_id": 1, "contact_display_name": "Dana White", "status": "pending"},
                        {"id": "s2", "from_user_id": 7, "to_user_id": 1, "contact_display_name": "Riley Stone", "status": "declined", "responded_at": "2026-08-01T10:00:00Z"}
                      ],
                      "usernames": {"7": "dana"},
                      "total": 2,
                      "next_cursor": "cursor-2",
                      "limit": 25
                    }
                    """.trimIndent(),
                ),
        )

        val result = client.listIncomingContactShares()

        assertTrue(result.isSuccess)
        val page = result.getOrThrow()
        assertEquals(2, page.contactShares.size)
        assertEquals("s1", page.contactShares[0].id)
        assertEquals(7, page.contactShares[0].fromUserId)
        assertEquals("Dana White", page.contactShares[0].contactDisplayName)
        assertEquals("pending", page.contactShares[0].status)
        assertEquals("dana", page.usernames["7"])
        assertEquals("cursor-2", page.nextCursor)

        val request = server.takeRequest()
        assertEquals("GET", request.method)
        assertEquals("/api/v1/contact-shares/incoming", request.path)
    }

    @Test
    fun `list incoming contact shares sends cursor and limit query parameters`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody("""{"contact_shares":[],"usernames":{},"total":0,"next_cursor":"","limit":25}"""),
        )

        client.listIncomingContactShares(cursor = "c1", limit = 10)

        val request = server.takeRequest()
        assertEquals("/api/v1/contact-shares/incoming?cursor=c1&limit=10", request.path)
    }

    @Test
    fun `list outgoing contact shares parses the cursor page`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """
                    {
                      "contact_shares": [
                        {"id": "s3", "from_user_id": 1, "to_user_id": 9, "contact_display_name": "Casey Tran", "status": "accepted", "responded_at": "2026-08-02T09:00:00Z"}
                      ],
                      "usernames": {"9": "casey"},
                      "total": 1,
                      "next_cursor": "",
                      "limit": 25
                    }
                    """.trimIndent(),
                ),
        )

        val result = client.listOutgoingContactShares()

        assertTrue(result.isSuccess)
        val page = result.getOrThrow()
        assertEquals(1, page.contactShares.size)
        assertEquals("s3", page.contactShares[0].id)
        assertEquals(9, page.contactShares[0].toUserId)
        assertEquals("accepted", page.contactShares[0].status)
        assertEquals("casey", page.usernames["9"])

        val request = server.takeRequest()
        assertEquals("GET", request.method)
        assertEquals("/api/v1/contact-shares/outgoing", request.path)
    }

    @Test
    fun `create contact share posts the input and unwraps the created share`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """{"message": "Share created", "contact_share":
                        {"id": "s1", "from_user_id": 1, "to_user_id": 9, "contact_display_name": "Casey Tran", "status": "pending"}}""",
                ),
        )

        val result = client.createContactShare(
            com.mycorrhizal.crm.model.network.ContactShareInput(
                toUserId = 9,
                vcardUid = "uid-1",
                sections = listOf("emails", "phones"),
                includeSensitive = true,
            ),
        )

        assertTrue(result.isSuccess)
        assertEquals("s1", result.getOrThrow().id)
        assertEquals("Casey Tran", result.getOrThrow().contactDisplayName)

        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/contact-shares", request.path)
        val body = request.body.readUtf8()
        assertTrue(body.contains("\"to_user_id\":9"))
        assertTrue(body.contains("\"vcard_uid\":\"uid-1\""))
        assertTrue(body.contains("\"include_sensitive\":true"))
    }

    @Test
    fun `accept contact share posts to the accept route and parses the preview`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """{"session_id": "import-1", "rows": [
                        {"row_index": 0, "parsed_contact": {"firstname": "Dana", "lastname": "White"},
                         "duplicate_match": {"existing_contact_id": 3, "existing_firstname": "Dana", "existing_lastname": "White", "match_reason": "name"},
                         "suggested_action": "update"}],
                        "total_rows": 1, "valid_rows": 1, "duplicate_count": 1, "error_count": 0}""",
                ),
        )

        val result = client.acceptContactShare("s1")

        assertTrue(result.isSuccess)
        val preview = result.getOrThrow()
        assertEquals("import-1", preview.sessionId)
        assertEquals(1, preview.rows.size)
        assertEquals("update", preview.rows[0].suggestedAction)
        assertEquals(3L, preview.rows[0].duplicateMatch?.existingContactId)
        assertEquals("name", preview.rows[0].duplicateMatch?.matchReason)

        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/contact-shares/s1/accept", request.path)
    }

    @Test
    fun `confirm contact share posts the session and per-row actions to the confirm route`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody("""{"total_processed": 1, "created": 1, "updated": 0, "skipped": 0, "errors": []}"""),
        )

        val result = client.confirmContactShare(
            "s1",
            com.mycorrhizal.crm.model.network.ImportConfirmRequest(
                sessionId = "import-1",
                actions = listOf(com.mycorrhizal.crm.model.network.RowImportAction(rowIndex = 0, action = "update")),
            ),
        )

        assertTrue(result.isSuccess)
        assertEquals(1, result.getOrThrow().created)

        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/contact-shares/s1/confirm", request.path)
        val body = request.body.readUtf8()
        assertTrue(body.contains("import-1"))
        assertTrue(body.contains("\"action\":\"update\""))
    }

    @Test
    fun `decline contact share posts to the decline route`() = runBlocking {
        server.enqueue(MockResponse().setResponseCode(200).setBody("""{"message": "Share declined"}"""))

        val result = client.declineContactShare("s1")

        assertTrue(result.isSuccess)
        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/contact-shares/s1/decline", request.path)
    }

    @Test
    fun `get user directory parses the users list`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"users": [{"id": 9, "username": "casey"}, {"id": 7, "username": "dana"}]}"""),
        )

        val result = client.getUserDirectory()

        assertTrue(result.isSuccess)
        val users = result.getOrThrow()
        assertEquals(2, users.size)
        assertEquals(9, users[0].id)
        assertEquals("casey", users[0].username)
        assertEquals("dana", users[1].username)

        val request = server.takeRequest()
        assertEquals("GET", request.method)
        assertEquals("/api/v1/users/directory", request.path)
    }


    // --- M25: settings surfaces (profile prefs, webhooks, notification channels) ---

    @Test
    fun `update language sends a PATCH to the language route with the body`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody("""{"message": "Language updated successfully", "language": "de"}"""),
        )

        val result = client.updateLanguage("de")

        assertTrue(result.isSuccess)
        assertEquals("Language updated successfully", result.getOrThrow().message)
        val request = server.takeRequest()
        assertEquals("PATCH", request.method)
        assertEquals("/api/v1/users/language", request.path)
        assertTrue(request.body.readUtf8().contains("\"language\":\"de\""))
    }

    @Test
    fun `update date format sends a PATCH to the date-format route with the body`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody("""{"message": "Date format updated successfully", "date_format": "us"}"""),
        )

        val result = client.updateDateFormat("us")

        assertTrue(result.isSuccess)
        assertEquals("Date format updated successfully", result.getOrThrow().message)
        val request = server.takeRequest()
        assertEquals("PATCH", request.method)
        assertEquals("/api/v1/users/date-format", request.path)
        assertTrue(request.body.readUtf8().contains("\"date_format\":\"us\""))
    }

    @Test
    fun `change password posts current and new password and parses the message`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody("""{"message": "Password updated successfully"}"""),
        )

        val result = client.changePassword("old-pass", "new-pass")

        assertTrue(result.isSuccess)
        assertEquals("Password updated successfully", result.getOrThrow().message)
        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/users/change-password", request.path)
        val body = request.body.readUtf8()
        assertTrue(body.contains("\"current_password\":\"old-pass\""))
        assertTrue(body.contains("\"new_password\":\"new-pass\""))
    }

    @Test
    fun `change password surfaces a wrong-current-password 400`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(400).setBody(
                """{"error":{"code":"INVALID_INPUT","message":"Invalid value for field 'current_password'",
                    "details":{"reason":"Current password is incorrect","field":"current_password"}}}""",
            ),
        )

        val result = client.changePassword("wrong", "new-pass")

        assertTrue(result.isFailure)
        val error = result.exceptionOrNull() as ApiError
        assertTrue(error is ApiError.Client)
        assertEquals(400, (error as ApiError.Client).code)
        assertTrue(error.displayMessage.contains("Invalid value for field 'current_password'"))
    }

    @Test
    fun `get notification config parses the flat config`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody(
                """{"ntfy_url":"https://ntfy.sh","ntfy_topic":"my-reminders",
                    "gotify_url":"http://gotify:8080","gotify_has_token":true,
                    "notify_ntfy":true,"notify_gotify":false,"notify_push":false,
                    "vapid_public_key":"key"}""",
            ),
        )

        val result = client.getNotificationConfig()

        assertTrue(result.isSuccess)
        val config = result.getOrThrow()
        assertEquals("https://ntfy.sh", config.ntfyUrl)
        assertEquals("my-reminders", config.ntfyTopic)
        assertTrue(config.gotifyHasToken)
        assertTrue(config.notifyNtfy)
        val request = server.takeRequest()
        assertEquals("/api/v1/notifications/config", request.path)
    }

    @Test
    fun `save notification config sends a PUT and parses the echoed config`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody(
                """{"ntfy_url":"https://ntfy.sh","ntfy_topic":"t","gotify_url":"","gotify_has_token":false,
                    "notify_ntfy":true,"notify_gotify":false,"notify_push":false,"vapid_public_key":"key"}""",
            ),
        )

        val result = client.saveNotificationConfig(
            com.mycorrhizal.crm.model.network.NotificationConfigInput(
                ntfyUrl = "https://ntfy.sh",
                ntfyTopic = "t",
                notifyNtfy = true,
            ),
        )

        assertTrue(result.isSuccess)
        assertEquals("https://ntfy.sh", result.getOrThrow().ntfyUrl)
        val request = server.takeRequest()
        assertEquals("PUT", request.method)
        assertEquals("/api/v1/notifications/config", request.path)
        val body = request.body.readUtf8()
        assertTrue(body.contains("\"ntfy_url\":\"https://ntfy.sh\""))
        // Unset fields are omitted, not serialized as null — the backend's pointer
        // semantics rely on absent keys keeping the stored value.
        assertTrue(!body.contains("notify_gotify"))
        assertTrue(!body.contains("gotify_token"))
    }

    @Test
    fun `test notification channel posts the channel and parses ok true`() = runBlocking {
        server.enqueue(MockResponse().setResponseCode(200).setBody("""{"ok": true}"""))

        val result = client.testNotificationChannel("ntfy")

        assertTrue(result.isSuccess)
        assertTrue(result.getOrThrow().ok)
        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/notifications/config/test", request.path)
        assertTrue(request.body.readUtf8().contains("\"channel\":\"ntfy\""))
    }

    @Test
    fun `test notification channel parses a diagnosed failure as ok false`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody(
                """{"ok": false, "error": "ntfy is not configured - set a URL and topic and save first"}""",
            ),
        )

        val result = client.testNotificationChannel("ntfy")

        assertTrue(result.isSuccess)
        assertTrue(!result.getOrThrow().ok)
        assertTrue(result.getOrThrow().error!!.contains("not configured"))
    }

    @Test
    fun `register device posts token client and label and parses the 201 row`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(201).setBody(
                """{"id":7,"created_at":"2026-08-18T10:00:00Z","updated_at":"2026-08-18T10:00:00Z","token":"fcm-token-abc","client":"fcm","device_label":"Pixel 8a"}""",
            ),
        )

        val result = client.registerDevice(
            DeviceRegistrationInput(token = "fcm-token-abc", client = "fcm", deviceLabel = "Pixel 8a"),
        )

        assertTrue(result.isSuccess)
        val device = result.getOrThrow()
        assertEquals(7, device.id)
        assertEquals("fcm", device.client)
        assertEquals("Pixel 8a", device.deviceLabel)

        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/notifications/devices", request.path)
        val body = request.body.readUtf8()
        assertTrue(body.contains("\"token\":\"fcm-token-abc\""))
        assertTrue(body.contains("\"client\":\"fcm\""))
        assertTrue(body.contains("\"device_label\":\"Pixel 8a\""))
    }

    @Test
    fun `delete device issues DELETE against the device id`() = runBlocking {
        server.enqueue(MockResponse().setResponseCode(200).setBody("""{"message":"Device registration deleted"}"""))

        val result = client.deleteDevice(7)

        assertTrue(result.isSuccess)
        val request = server.takeRequest()
        assertEquals("DELETE", request.method)
        assertEquals("/api/v1/notifications/devices/7", request.path)
    }

    @Test
    fun `list device registrations unwraps the devices array`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody(
                """{"devices":[
                    {"id":1,"created_at":"2026-08-01T00:00:00Z","updated_at":"2026-08-01T00:00:00Z","token":"a","client":"fcm","device_label":"Pixel"},
                    {"id":2,"created_at":"2026-08-02T00:00:00Z","updated_at":"2026-08-02T00:00:00Z","token":"b","client":"apns","device_label":"iPhone"}
                ]}""",
            ),
        )

        val result = client.listDeviceRegistrations()

        assertTrue(result.isSuccess)
        assertEquals(2, result.getOrThrow().size)
        assertEquals("fcm", result.getOrThrow()[0].client)
        assertEquals("iPhone", result.getOrThrow()[1].deviceLabel)
        val request = server.takeRequest()
        assertEquals("GET", request.method)
        assertEquals("/api/v1/notifications/devices", request.path)
    }

    @Test
    fun `list webhooks parses and unwraps the webhooks array`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody(
                """{"webhooks":[
                    {"id":1,"name":"Hook A","url":"https://example.com/a","events":["contact.created"],"is_active":true,"created_at":"2026-08-01T00:00:00Z"},
                    {"id":2,"name":"Hook B","url":"https://example.com/b","events":["note.created","note.updated"],"is_active":false,"created_at":"2026-08-02T00:00:00Z"}
                ]}""",
            ),
        )

        val result = client.listWebhooks()

        assertTrue(result.isSuccess)
        val webhooks = result.getOrThrow()
        assertEquals(2, webhooks.size)
        assertEquals("Hook A", webhooks[0].name)
        assertTrue(webhooks[0].isActive)
        assertEquals(listOf("note.created", "note.updated"), webhooks[1].events)
        val request = server.takeRequest()
        assertEquals("/api/v1/webhooks", request.path)
    }

    @Test
    fun `create webhook posts the input and returns the secret once`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(201).setBody(
                """{"id": 9, "name": "Hook A", "url": "https://example.com/a",
                    "events": ["contact.created"], "is_active": true,
                    "created_at": "2026-08-01T00:00:00Z", "secret": "base64secret"}""",
            ),
        )

        val result = client.createWebhook(
            com.mycorrhizal.crm.model.network.WebhookInput(
                name = "Hook A",
                url = "https://example.com/a",
                events = listOf("contact.created"),
                isActive = true,
            ),
        )

        assertTrue(result.isSuccess)
        assertEquals(9, result.getOrThrow().id)
        assertEquals("base64secret", result.getOrThrow().secret)
        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/webhooks", request.path)
        val body = request.body.readUtf8()
        assertTrue(body.contains("\"name\":\"Hook A\""))
        assertTrue(body.contains("\"is_active\":true"))
    }

    @Test
    fun `update webhook sends a PUT and parses the raw webhook`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody(
                """{"id": 9, "name": "Hook A2", "url": "https://example.com/a2",
                    "events": ["contact.created","contact.updated"], "is_active": false,
                    "created_at": "2026-08-01T00:00:00Z"}""",
            ),
        )

        val result = client.updateWebhook(
            9,
            com.mycorrhizal.crm.model.network.WebhookInput(
                name = "Hook A2",
                url = "https://example.com/a2",
                events = listOf("contact.created", "contact.updated"),
                isActive = false,
            ),
        )

        assertTrue(result.isSuccess)
        assertEquals("Hook A2", result.getOrThrow().name)
        assertTrue(!result.getOrThrow().isActive)
        assertEquals(2, result.getOrThrow().events.size)
        val request = server.takeRequest()
        assertEquals("PUT", request.method)
        assertEquals("/api/v1/webhooks/9", request.path)
        assertTrue(request.body.readUtf8().contains("\"is_active\":false"))
    }

    @Test
    fun `delete webhook sends a DELETE to the webhook route`() = runBlocking {
        server.enqueue(MockResponse().setResponseCode(200).setBody("""{"message": "Webhook deleted"}"""))

        val result = client.deleteWebhook(9)

        assertTrue(result.isSuccess)
        val request = server.takeRequest()
        assertEquals("DELETE", request.method)
        assertEquals("/api/v1/webhooks/9", request.path)
    }

    @Test
    fun `test webhook posts and unwraps the delivery`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody(
                """{"delivery": {"id": 1, "webhook_id": 9, "event_type": "test", "status_code": 200,
                    "error": null, "attempts": 1, "next_retry_at": null, "created_at": "2026-08-01T00:00:00Z"}}""",
            ),
        )

        val result = client.testWebhook(9)

        assertTrue(result.isSuccess)
        assertEquals("test", result.getOrThrow().eventType)
        assertEquals(200, result.getOrThrow().statusCode)
        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/webhooks/9/test", request.path)
    }

    @Test
    fun `test webhook parses a failed delivery with a status code and error`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody(
                """{"delivery": {"id": 2, "webhook_id": 9, "event_type": "test", "status_code": 500,
                    "error": "connection refused", "attempts": 3, "next_retry_at": "2026-08-01T01:00:00Z",
                    "created_at": "2026-08-01T00:00:00Z"}}""",
            ),
        )

        val result = client.testWebhook(9)

        assertTrue(result.isSuccess)
        val delivery = result.getOrThrow()
        assertEquals(500, delivery.statusCode)
        assertEquals("connection refused", delivery.error)
        assertEquals(3, delivery.attempts)
        assertNotNull(delivery.nextRetryAt)
    }

    @Test
    fun `get webhook deliveries parses and unwraps the deliveries array`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody(
                """{"deliveries":[
                    {"id": 1, "webhook_id": 9, "event_type": "contact.created", "status_code": 200,
                     "error": null, "attempts": 1, "next_retry_at": null, "created_at": "2026-08-01T00:00:00Z"},
                    {"id": 2, "webhook_id": 9, "event_type": "test", "status_code": 500,
                     "error": "boom", "attempts": 2, "next_retry_at": null, "created_at": "2026-08-01T00:01:00Z"}
                ]}""",
            ),
        )

        val result = client.getWebhookDeliveries(9)

        assertTrue(result.isSuccess)
        val deliveries = result.getOrThrow()
        assertEquals(2, deliveries.size)
        assertEquals("contact.created", deliveries[0].eventType)
        assertEquals(500, deliveries[1].statusCode)
        assertEquals("boom", deliveries[1].error)
        val request = server.takeRequest()
        assertEquals("/api/v1/webhooks/9/deliveries", request.path)
    }

    @Test
    fun `get webhook deliveries normalizes an empty array`() = runBlocking {
        server.enqueue(MockResponse().setResponseCode(200).setBody("""{"deliveries": []}"""))

        val result = client.getWebhookDeliveries(9)

        assertTrue(result.isSuccess)
        assertTrue(result.getOrThrow().isEmpty())
    }

    // --- Issue #413 / #573: API token lifecycle ---

    @Test
    fun `list api tokens parses and unwraps the tokens array`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody(
                """{"tokens":[
                    {"id":1,"name":"Sync script","created_at":"2026-08-01T00:00:00Z","last_used_at":null,"revoked_at":null,"expires_at":"2026-11-01T00:00:00Z","scope":"full"},
                    {"id":2,"name":"Old token","created_at":"2026-07-01T00:00:00Z","last_used_at":"2026-08-01T00:00:00Z","revoked_at":"2026-08-02T00:00:00Z","expires_at":null,"scope":"carddav"}
                ]}""",
            ),
        )

        val result = client.listApiTokens()

        assertTrue(result.isSuccess)
        val tokens = result.getOrThrow()
        assertEquals(2, tokens.size)
        assertEquals("Sync script", tokens[0].name)
        assertEquals("full", tokens[0].scope)
        assertNotNull(tokens[1].revokedAt)
        val request = server.takeRequest()
        assertEquals("/api/v1/api-tokens", request.path)
    }

    @Test
    fun `create api token posts the input and returns the plaintext once`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(201).setBody(
                """{"id": 9, "name": "New token", "created_at": "2026-08-01T00:00:00Z",
                    "last_used_at": null, "revoked_at": null, "expires_at": "2026-11-01T00:00:00Z",
                    "scope": "full", "token": "mcrh_live_abc123"}""",
            ),
        )

        val result = client.createApiToken(
            com.mycorrhizal.crm.model.network.ApiTokenInput(name = "New token", expiresInDays = 90, scope = "full"),
        )

        assertTrue(result.isSuccess)
        assertEquals(9, result.getOrThrow().id)
        assertEquals("mcrh_live_abc123", result.getOrThrow().token)
        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/api-tokens", request.path)
        val body = request.body.readUtf8()
        assertTrue(body.contains("\"name\":\"New token\""))
        assertTrue(body.contains("\"expires_in_days\":90"))
    }

    @Test
    fun `revoke api token sends a DELETE to the token route`() = runBlocking {
        server.enqueue(MockResponse().setResponseCode(200).setBody("""{"message": "Token revoked successfully"}"""))

        val result = client.revokeApiToken(9)

        assertTrue(result.isSuccess)
        val request = server.takeRequest()
        assertEquals("DELETE", request.method)
        assertEquals("/api/v1/api-tokens/9", request.path)
    }

    @Test
    fun `revoke all api tokens posts an empty body and returns the revoked count`() = runBlocking {
        server.enqueue(MockResponse().setResponseCode(200).setBody("""{"revoked": 3}"""))

        val result = client.revokeAllApiTokens()

        assertTrue(result.isSuccess)
        assertEquals(3, result.getOrThrow().revoked)
        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/api-tokens/revoke-all", request.path)
        assertEquals(0L, request.bodySize)
    }

    @Test
    fun `rotate api token posts an empty body and returns the new plaintext once`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(201).setBody(
                """{"id": 10, "name": "Sync script", "created_at": "2026-08-02T00:00:00Z",
                    "last_used_at": null, "revoked_at": null, "expires_at": "2026-11-02T00:00:00Z",
                    "scope": "full", "token": "mcrh_live_rotated456"}""",
            ),
        )

        val result = client.rotateApiToken(9)

        assertTrue(result.isSuccess)
        assertEquals(10, result.getOrThrow().id)
        assertEquals("mcrh_live_rotated456", result.getOrThrow().token)
        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/api-tokens/9/rotate", request.path)
        assertEquals(0L, request.bodySize)
    }

    // --- M16: audit trail ---

    @Test
    fun `get audit events parses the event list and defaults the limit`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """
                    {
                      "audit_events": [
                        {"id": 3, "created_at": "2026-08-14T10:00:00Z", "entity_type": "contact", "entity_id": "uid-1", "operation": "update", "before_snapshot": "{}"},
                        {"id": 2, "created_at": "2026-08-14T09:00:00Z", "entity_type": "note", "entity_id": "7", "operation": "create"},
                        {"id": 1, "created_at": "2026-08-14T08:00:00Z", "entity_type": "contact", "entity_id": "uid-2", "operation": "delete", "before_snapshot": "{}"}
                      ],
                      "total": 3
                    }
                    """.trimIndent(),
                ),
        )

        val result = client.getAuditEvents()

        assertTrue(result.isSuccess)
        val page = result.getOrThrow()
        assertEquals(3, page.auditEvents.size)
        assertEquals(3, page.total)
        val first = page.auditEvents[0]
        assertEquals(3L, first.id)
        assertEquals("contact", first.entityType)
        assertEquals("update", first.operation)
        assertTrue(first.canUndo)
        assertFalse(page.auditEvents[2].canUndo)
        assertEquals("2026-08-14T10:00:00Z", first.createdAt)

        val request = server.takeRequest()
        assertEquals("GET", request.method)
        assertEquals("/api/v1/audit?limit=100", request.path)
    }

    @Test
    fun `get audit events sends the entity filters on the query string`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody("""{"audit_events":[],"total":0}"""),
        )

        client.getAuditEvents(entityType = "contact", entityId = "uid-1", limit = 250)

        val request = server.takeRequest()
        assertEquals("GET", request.method)
        assertEquals("/api/v1/audit?limit=250&entity_type=contact&entity_id=uid-1", request.path)
    }

    @Test
    fun `get audit events omits blank filters`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody("""{"audit_events":[],"total":0}"""),
        )

        client.getAuditEvents(entityType = "  ", entityId = "")

        val request = server.takeRequest()
        assertEquals("/api/v1/audit?limit=100", request.path)
    }

    @Test
    fun `undo audit event posts to the event's undo route`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody("""{"message": "Contact restored to its previous state"}"""),
        )

        val result = client.undoAuditEvent(42L)

        assertTrue(result.isSuccess)
        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/audit/42/undo", request.path)
    }

    @Test
    fun `undo audit event maps a 410 to a client error with the backend message`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(410).setBody("""{"error":{"code":"gone","message":"past retention window"}}"""),
        )

        val result = client.undoAuditEvent(42L)

        assertTrue(result.isFailure)
        val error = result.exceptionOrNull() as ApiError
        assertTrue(error is ApiError.Client)
        assertEquals(410, (error as ApiError.Client).code)
    }


    // --- M22: household relationship & shared-address suggestions ---

    @Test
    fun `suggest household relationships posts an empty body and parses the response`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """
                    {
                      "message": "Relationship suggestions generated",
                      "household_id": "h1",
                      "suggested_edges": [
                        {"id": "e1", "source_id": "u1", "target_id": "u2", "type": "spouse_of", "status": "suggested", "source": "household-inferred", "confidence": 0.8}
                      ],
                      "total": 1
                    }
                    """.trimIndent(),
                ),
        )

        val result = client.suggestHouseholdRelationships("h1")

        assertTrue(result.isSuccess)
        assertEquals("h1", result.getOrThrow().householdId)
        assertEquals(1, result.getOrThrow().suggestedEdges.size)
        assertEquals("spouse_of", result.getOrThrow().suggestedEdges[0].type)
        assertEquals("suggested", result.getOrThrow().suggestedEdges[0].status)

        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/households/h1/suggest-relationships", request.path)
        assertEquals(0, request.body.size)
    }

    @Test
    fun `suggest household relationships tolerates an absent suggested_edges key`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """{"message": "Relationship suggestions generated", "household_id": "h1", "total": 0}""",
                ),
        )

        val result = client.suggestHouseholdRelationships("h1")

        assertTrue(result.isSuccess)
        assertTrue(result.getOrThrow().suggestedEdges.isEmpty())
    }

    @Test
    fun `suggest address households parses the suggestions`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """
                    {
                      "suggestions": [
                        {
                          "address_hash": "ah1",
                          "member_hash": "mh1",
                          "member_vcard_uids": ["u1", "u2"],
                          "address": {"components": [{"kind": "locality", "value": "Berlin"}], "full": "1 Main St, Berlin"}
                        }
                      ],
                      "total": 1
                    }
                    """.trimIndent(),
                ),
        )

        val result = client.suggestAddressHouseholds()

        assertTrue(result.isSuccess)
        val suggestions = result.getOrThrow().suggestions
        assertEquals(1, suggestions.size)
        assertEquals(listOf("u1", "u2"), suggestions[0].memberVCardUids)
        assertEquals("Berlin", suggestions[0].address?.components?.get(0)?.value)

        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/households/suggest-addresses", request.path)
    }

    @Test
    fun `suggest address households tolerates an absent suggestions key`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"total": 0}"""),
        )

        val result = client.suggestAddressHouseholds()

        assertTrue(result.isSuccess)
        assertTrue(result.getOrThrow().suggestions.isEmpty())
    }

    @Test
    fun `accept household suggestion posts member uids and unwraps the household`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(201)
                .setBody(
                    """{"household": {"id": "h9", "name": "Alice & Bob", "type": "family_unit"}}""",
                ),
        )

        val input = com.mycorrhizal.crm.model.network.AcceptHouseholdSuggestionInput(
            memberVCardUids = listOf("u1", "u2"),
        )
        val result = client.acceptHouseholdSuggestion(input)

        assertTrue(result.isSuccess)
        assertEquals("h9", result.getOrThrow().id)
        assertEquals("Alice & Bob", result.getOrThrow().name)

        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/households/suggestions/accept", request.path)
        val body = request.body.readUtf8()
        assertTrue(body.contains("member_vcard_uids"))
        assertTrue(body.contains("u1"))
        assertTrue(body.contains("u2"))
    }

    @Test
    fun `dismiss household suggestion posts member uids`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"message": "Household suggestion dismissed"}"""),
        )

        val input = com.mycorrhizal.crm.model.network.DismissHouseholdSuggestionInput(
            memberVCardUids = listOf("u1", "u2"),
        )
        val result = client.dismissHouseholdSuggestion(input)

        assertTrue(result.isSuccess)

        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/households/suggestions/dismiss", request.path)
        val body = request.body.readUtf8()
        assertTrue(body.contains("member_vcard_uids"))
        assertTrue(body.contains("u1"))
    }

    // --- T104: graph-inferred relationship suggestions + address suggestions ---

    @Test
    fun `suggest relationship edges posts an empty body to the suggest path`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """
                    {
                      "message": "Relationship suggestions generated",
                      "suggested_edges": [
                        {"id": "e1", "source_id": "u1", "target_id": "u2", "type": "parent_of", "status": "suggested", "source": "graph-inferred", "confidence": 0.7}
                      ],
                      "total": 1
                    }
                    """.trimIndent(),
                ),
        )

        val result = client.suggestRelationshipEdges()

        assertTrue(result.isSuccess)
        assertEquals(1, result.getOrThrow().suggestedEdges.size)
        assertEquals("graph-inferred", result.getOrThrow().suggestedEdges[0].source)
        assertEquals("parent_of", result.getOrThrow().suggestedEdges[0].type)

        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/relationship-edges/suggest", request.path)
        assertEquals(0, request.body.size)
    }

    @Test
    fun `suggest relationship edges tolerates an absent suggested_edges key`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"message": "Relationship suggestions generated", "total": 0}"""),
        )

        val result = client.suggestRelationshipEdges()

        assertTrue(result.isSuccess)
        assertTrue(result.getOrThrow().suggestedEdges.isEmpty())
    }

    @Test
    fun `suggest contact addresses parses the suggestions`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """
                    {
                      "suggestions": [
                        {
                          "contact_vcard_uid": "alice-uid",
                          "contact_name": "Alice Anderson",
                          "source_kind": "relationship",
                          "source_id": "bob-uid",
                          "source_name": "Bob Brown",
                          "relation_type": "spouse_of",
                          "address": {"components": [{"kind": "name", "value": "742 Clark St"}], "full": "742 Clark St, Springfield"},
                          "address_key": "742 clark st|springfield"
                        }
                      ],
                      "total": 1
                    }
                    """.trimIndent(),
                ),
        )

        val result = client.suggestContactAddresses()

        assertTrue(result.isSuccess)
        val suggestions = result.getOrThrow().suggestions
        assertEquals(1, suggestions.size)
        assertEquals("alice-uid", suggestions[0].contactVCardUid)
        assertEquals("spouse_of", suggestions[0].relationType)
        assertEquals("742 clark st|springfield", suggestions[0].addressKey)

        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/contacts/address-suggestions", request.path)
        assertEquals(0, request.body.size)
    }

    @Test
    fun `suggest contact addresses tolerates an absent suggestions key`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"total": 0}"""),
        )

        val result = client.suggestContactAddresses()

        assertTrue(result.isSuccess)
        assertTrue(result.getOrThrow().suggestions.isEmpty())
    }

    @Test
    fun `apply contact address suggestion posts the suggestion identity`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"message": "Address added to contact", "contact_vcard_uid": "alice-uid"}"""),
        )

        val input = com.mycorrhizal.crm.model.network.ApplyContactAddressSuggestionInput(
            contactVCardUid = "alice-uid",
            sourceKind = "relationship",
            sourceId = "bob-uid",
            addressKey = "742 clark st|springfield",
        )
        val result = client.applyContactAddressSuggestion(input)

        assertTrue(result.isSuccess)

        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/contacts/address-suggestions/apply", request.path)
        val body = request.body.readUtf8()
        assertTrue(body.contains("alice-uid"))
        assertTrue(body.contains("relationship"))
        assertTrue(body.contains("742 clark st|springfield"))
    }

    // --- M14: ego-centric network graph ---

    @Test
    fun `getConnections sends from, depth and relation on the query string`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """
                    {
                      "from_vcard_uid": "from-1",
                      "from_name": "Alice",
                      "depth": 3,
                      "chains": []
                    }
                    """.trimIndent(),
                ),
        )

        // relation is a registry synonym — it must be passed through verbatim,
        // never resolved on-device (M14 design: the server resolves it).
        val result = client.getConnections(from = "from-1", depth = 3, relation = "brother")

        assertTrue(result.isSuccess)
        assertEquals("from-1", result.getOrThrow().fromVCardUid)

        val request = server.takeRequest()
        assertEquals("GET", request.method)
        assertEquals("/api/v1/graph/connections?from=from-1&depth=3&relation=brother", request.path)
    }

    @Test
    fun `getConnections omits depth and relation when not set`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"from_vcard_uid":"from-1","from_name":"Alice","depth":2,"chains":[]}"""),
        )

        val result = client.getConnections(from = "from-1")

        assertTrue(result.isSuccess)
        assertEquals("/api/v1/graph/connections?from=from-1", server.takeRequest().path)
    }

    @Test
    fun `getConnections parses resolved names, inverse relations and step chains`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """
                    {
                      "from_vcard_uid": "from-1",
                      "from_name": "Alice",
                      "depth": 2,
                      "chains": [
                        {
                          "target_id": 10,
                          "target_vcard_uid": "t1",
                          "target_name": "Carol",
                          "depth": 1,
                          "steps": [
                            {"contact_id": 10, "contact_vcard_uid": "t1", "contact_name": "Carol", "relation": "child_of"}
                          ]
                        },
                        {
                          "target_id": 20,
                          "target_vcard_uid": "t2",
                          "target_name": "Dave",
                          "depth": 2,
                          "steps": [
                            {"contact_id": 11, "contact_vcard_uid": "t1", "contact_name": "Carol", "relation": "sibling_of"},
                            {"contact_id": 20, "contact_vcard_uid": "t2", "contact_name": "Dave", "relation": "spouse_of"}
                          ]
                        }
                      ]
                    }
                    """.trimIndent(),
                ),
        )

        val result = client.getConnections(from = "from-1")

        assertTrue(result.isSuccess)
        val chains = result.getOrThrow().chainsOrEmpty
        assertEquals(2, chains.size)
        assertEquals("Carol", chains[0].targetName)
        assertEquals("child_of", chains[0].stepsOrEmpty[0].relation)
        assertEquals(2, chains[1].stepsOrEmpty.size)
        assertEquals("Dave", chains[1].stepsOrEmpty[1].contactName)
    }

    @Test
    fun `getConnections tolerates an absent chains key`() = runBlocking {
        // The backend always serializes [], but the contract must not break on
        // an absent key either (`/CLAUDE.md` frontend trap #8).
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"from_vcard_uid":"from-1","from_name":"Alice","depth":2}"""),
        )

        val result = client.getConnections(from = "from-1")

        assertTrue(result.isSuccess)
        assertTrue(result.getOrThrow().chainsOrEmpty.isEmpty())
    }

    @Test
    fun `getConnections tolerates an explicit null chains value`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"from_vcard_uid":"from-1","from_name":"Alice","depth":2,"chains":null}"""),
        )

        val result = client.getConnections(from = "from-1")

        assertTrue(result.isSuccess)
        assertTrue(result.getOrThrow().chainsOrEmpty.isEmpty())
    }

    @Test
    fun `getConnections maps a 404 unknown-from to a Client error`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(404)
                .setBody("""{"error":{"code":"NOT_FOUND","message":"Contact not found","details":{"vcard_uid":"ghost"}}}"""),
        )

        val result = client.getConnections(from = "ghost")

        assertTrue(result.isFailure)
        val error = result.exceptionOrNull() as ApiError
        assertTrue(error is ApiError.Client)
        assertEquals(404, (error as ApiError.Client).code)
    }

    @Test
    fun `currentUser parses the self contact vcard uid`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """{"id":1,"username":"alice","email":"a@x.com","language":"en","date_format":"eu","is_admin":false,"self_contact_vcard_uid":"self-uid"}""",
                ),
        )

        val result = client.currentUser()

        assertTrue(result.isSuccess)
        assertEquals("self-uid", result.getOrThrow().selfContactVCardUid)
    }

    // --- M18: conversation-agenda discuss sends the activity link ---

    @Test
    fun `discussConversationAgenda with an activity sends activity_id`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"id":"a1","entity_id":"uid","content":"Ask","activity_id":7,"discussed_at":"2026-08-01T10:00:00Z"}"""),
        )

        val result = client.discussConversationAgenda("a1", activityId = 7)

        assertTrue(result.isSuccess)
        assertEquals(7, result.getOrThrow().activityId)
        val request = server.takeRequest()
        assertEquals("PATCH", request.method)
        assertEquals("/api/v1/conversation-agenda/a1/discuss", request.path)
        // Moshi omits nulls by default, so an explicit id serializes as {activity_id: 7}.
        assertTrue(request.body.readUtf8().contains("""{"activity_id":7}"""))
    }

    @Test
    fun `discussConversationAgenda without an activity sends an empty body`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"id":"a1","entity_id":"uid","content":"Ask"}"""),
        )

        val result = client.discussConversationAgenda("a1", activityId = null)

        assertTrue(result.isSuccess)
        assertNull(result.getOrThrow().activityId)
        val request = server.takeRequest()
        assertEquals("PATCH", request.method)
        // Marking discussed without a link is an empty object — matching web's
        // MarkDiscussedDialog (`{}` when unlinked).
        assertEquals("{}", request.body.readUtf8())
    }

    // --- M26: registration + password reset ---

    @Test
    fun `register posts username email and password`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(201)
                .setBody("""{"message":"User registered successfully"}"""),
        )

        val result = client.register("alice", "alice@example.com", "hunter2hunter2")

        assertTrue(result.isSuccess)
        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/register", request.path)
        val body = request.body.readUtf8()
        assertTrue(body.contains("""{"username":"alice","email":"alice@example.com","password":"hunter2hunter2"}"""))
    }

    @Test
    fun `register surfaces the duplicate-account conflict message`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(409)
                .setBody("""{"error":{"code":"ALREADY_EXISTS","message":"User already exists","details":{"email":"alice@example.com"}}}"""),
        )

        val result = client.register("alice", "alice@example.com", "hunter2hunter2")

        assertTrue(result.isFailure)
        val error = result.exceptionOrNull() as ApiError
        assertTrue(error is ApiError.Client)
        assertEquals(409, (error as ApiError.Client).code)
    }

    @Test
    fun `checkPasswordStrength posts the password and parses the verdict`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """{"is_valid":false,"entropy":18.3,"score":1,"feedback":"Password is too short","min_entropy":50.0,"char_set_size":26,"length":8}""",
                ),
        )

        val result = client.checkPasswordStrength("shortpw")

        assertTrue(result.isSuccess)
        val strength = result.getOrThrow()
        assertFalse(strength.isValid)
        assertEquals(18.3, strength.entropy!!, 0.001)
        assertEquals("Password is too short", strength.feedback)
        val request = server.takeRequest()
        assertEquals("/api/v1/check-password-strength", request.path)
        assertTrue(request.body.readUtf8().contains("""{"password":"shortpw"}"""))
    }

    @Test
    fun `requestPasswordReset posts the email and reads the anti-enumeration message`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"message":"If an account exists, password reset instructions were sent"}"""),
        )

        val result = client.requestPasswordReset("alice@example.com")

        assertTrue(result.isSuccess)
        assertEquals("If an account exists, password reset instructions were sent", result.getOrThrow().message)
        val request = server.takeRequest()
        assertEquals("/api/v1/password-reset/request", request.path)
        assertTrue(request.body.readUtf8().contains("""{"email":"alice@example.com"}"""))
    }

    @Test
    fun `confirmPasswordReset posts the token and password`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"message":"Password reset successful"}"""),
        )

        val result = client.confirmPasswordReset("reset-token-123", "newpassword123")

        assertTrue(result.isSuccess)
        val request = server.takeRequest()
        assertEquals("/api/v1/password-reset/confirm", request.path)
        val body = request.body.readUtf8()
        assertTrue(body.contains("""{"token":"reset-token-123","password":"newpassword123"}"""))
    }

    // --- M26: circle/tag triage ---

    @Test
    fun `listLegacyCircles parses a bare JSON array`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""["Friends from school","Ski club"]"""),
        )

        val result = client.listLegacyCircles()

        assertTrue(result.isSuccess)
        assertEquals(listOf("Friends from school", "Ski club"), result.getOrThrow())
        val request = server.takeRequest()
        assertEquals("/api/v1/contacts/circles?legacy=true", request.path)
    }

    @Test
    fun `listContacts sends circle_legacy for the triage contact lookup`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"contacts":[{"id":1,"uid":"u1","fn":"Alice"}],"limit":500}"""),
        )

        val result = client.listContacts(circleLegacy = "Ski club", limit = 500)

        assertTrue(result.isSuccess)
        assertEquals(1, result.getOrThrow().contacts.size)
        val request = server.takeRequest()
        assertEquals("/api/v1/contacts?limit=500&circle_legacy=Ski%20club", request.path)
    }

    // --- Admin user management (issue #348) ---

    @Test
    fun `listUsers sends the page and limit query params and parses the page`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody(
                """
                {"users": [
                   {"id": 1, "username": "alice", "email": "alice@example.com",
                    "language": "en", "date_format": "eu", "is_admin": true,
                    "created_at": "2026-08-01T00:00:00Z", "updated_at": "2026-08-01T00:00:00Z"}
                 ], "total": 1, "page": 1, "limit": 100, "total_pages": 1}
                """.trimIndent(),
            ),
        )

        val result = client.listUsers(page = 1, limit = 100)

        assertTrue(result.isSuccess)
        val page = result.getOrThrow()
        assertEquals(1, page.total)
        assertEquals("alice", page.users[0].username)
        assertTrue(page.users[0].isAdmin)
        val request = server.takeRequest()
        assertEquals("GET", request.method)
        assertEquals("/api/v1/admin/users?page=1&limit=100", request.path)
    }

    @Test
    fun `createUser posts the input to the admin route and parses the user`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(201).setBody(
                """{"id": 3, "username": "bob", "email": "bob@example.com",
                    "language": "en", "date_format": "eu", "is_admin": false,
                    "created_at": "2026-08-02T00:00:00Z", "updated_at": "2026-08-02T00:00:00Z"}""",
            ),
        )

        val result = client.createUser(
            com.mycorrhizal.crm.model.network.AdminUserCreateInput(
                username = "bob",
                email = "bob@example.com",
                password = "password123",
                isAdmin = false,
            ),
        )

        assertTrue(result.isSuccess)
        assertEquals(3, result.getOrThrow().id)
        assertEquals("bob", result.getOrThrow().username)
        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/admin/users", request.path)
        val body = request.body.readUtf8()
        assertTrue(body.contains("\"username\":\"bob\""))
        assertTrue(body.contains("\"password\":\"password123\""))
        assertTrue(body.contains("\"is_admin\":false"))
    }

    @Test
    fun `getUser parses a single user`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody(
                """{"id": 3, "username": "bob", "email": "bob@example.com",
                    "language": "de", "date_format": "eu", "is_admin": true,
                    "created_at": "2026-08-02T00:00:00Z", "updated_at": "2026-08-02T00:00:00Z"}""",
            ),
        )

        val result = client.getUser(3)

        assertTrue(result.isSuccess)
        assertEquals(3, result.getOrThrow().id)
        assertTrue(result.getOrThrow().isAdmin)
        val request = server.takeRequest()
        assertEquals("GET", request.method)
        assertEquals("/api/v1/admin/users/3", request.path)
    }

    @Test
    fun `updateUser sends a PATCH and only includes the provided fields`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody(
                """{"id": 3, "username": "bob2", "email": "bob@example.com",
                    "language": "en", "date_format": "eu", "is_admin": true,
                    "created_at": "2026-08-02T00:00:00Z", "updated_at": "2026-08-03T00:00:00Z"}""",
            ),
        )

        val result = client.updateUser(
            3,
            com.mycorrhizal.crm.model.network.AdminUserUpdateInput(
                username = "bob2",
                isAdmin = true,
            ),
        )

        assertTrue(result.isSuccess)
        assertEquals("bob2", result.getOrThrow().username)
        val request = server.takeRequest()
        assertEquals("PATCH", request.method)
        assertEquals("/api/v1/admin/users/3", request.path)
        val body = request.body.readUtf8()
        assertTrue(body.contains("\"username\":\"bob2\""))
        assertTrue(body.contains("\"is_admin\":true"))
        // Null fields are omitted — the password is not part of this edit.
        assertFalse(body.contains("password"))
    }

    @Test
    fun `updateUser sends is_admin false when explicitly set`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody(
                """{"id": 3, "username": "bob", "email": "bob@example.com",
                    "language": "en", "date_format": "eu", "is_admin": false,
                    "created_at": "2026-08-02T00:00:00Z", "updated_at": "2026-08-03T00:00:00Z"}""",
            ),
        )

        val result = client.updateUser(
            3,
            com.mycorrhizal.crm.model.network.AdminUserUpdateInput(isAdmin = false),
        )

        assertTrue(result.isSuccess)
        assertFalse(result.getOrThrow().isAdmin)
        val request = server.takeRequest()
        assertTrue(request.body.readUtf8().contains("\"is_admin\":false"))
    }

    @Test
    fun `deleteUser sends a DELETE to the admin route`() = runBlocking {
        server.enqueue(MockResponse().setResponseCode(200).setBody("""{"message": "User deleted successfully"}"""))

        val result = client.deleteUser(3)

        assertTrue(result.isSuccess)
        val request = server.takeRequest()
        assertEquals("DELETE", request.method)
        assertEquals("/api/v1/admin/users/3", request.path)
    }

    @Test
    fun `deleteUser surfaces a server-side guard as a client error`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(403).setBody(
                """{"error":{"code":"forbidden","message":"Cannot delete the last admin"}}""",
            ),
        )

        val result = client.deleteUser(1)

        assertTrue(result.isFailure)
        val error = result.exceptionOrNull() as ApiError
        assertTrue(error is ApiError.Client)
        assertEquals(403, (error as ApiError.Client).code)
    }
}
