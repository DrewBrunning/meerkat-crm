package com.mycorrhizal.crm.data.repository

import com.mycorrhizal.crm.data.session.DefaultSessionManager
import com.mycorrhizal.crm.data.session.FakeSessionPrefsStorage
import com.mycorrhizal.crm.data.session.FakeTokenStorage
import com.mycorrhizal.crm.domain.repository.LoginOutcome
import com.mycorrhizal.crm.domain.repository.SessionState
import com.mycorrhizal.crm.model.network.MessageResponse
import com.mycorrhizal.crm.model.network.TwoFactorConfirmResponse
import com.mycorrhizal.crm.model.network.UserProfile
import com.mycorrhizal.crm.network.ApiClient
import com.mycorrhizal.crm.network.ApiError
import com.mycorrhizal.crm.network.LoginResult
import com.mycorrhizal.crm.network.ReissuedTokenResult
import io.mockk.coEvery
import io.mockk.coVerify
import io.mockk.mockk
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class AuthRepositoryImplTest {

    private class Harness {
        val apiClient = mockk<ApiClient>()
        val tokenStorage = FakeTokenStorage()
        val sessionManager = DefaultSessionManager(tokenStorage, FakeSessionPrefsStorage())
        val repository = AuthRepositoryImpl(apiClient, sessionManager)
    }

    @Test
    fun `login persists the token and profile`() = runTest {
        val h = Harness()
        h.sessionManager.setServerUrl("https://crm.example.com")
        coEvery { h.apiClient.login(any(), any()) } returns Result.success(
            LoginResult(token = "jwt-123", language = "en", dateFormat = "eu"),
        )
        coEvery { h.apiClient.currentUser() } returns Result.success(
            UserProfile(id = 7, username = "alice", isAdmin = true, language = "en"),
        )

        val result = h.repository.login("alice", "secret")

        assertTrue(result.isSuccess)
        assertEquals("jwt-123", h.tokenStorage.stored)
        assertEquals("jwt-123", h.sessionManager.bearerToken())
        val state = h.sessionManager.observeSession().first()
        assertTrue(state.isLoggedIn)
        assertEquals(7, state.userId)
        assertEquals("alice", state.username)
        assertTrue(state.isAdmin)
    }

    @Test
    fun `login persists the token before fetching the profile`() = runTest {
        val h = Harness()
        h.sessionManager.setServerUrl("https://crm.example.com")
        coEvery { h.apiClient.login(any(), any()) } returns Result.success(
            LoginResult(token = "jwt-123", language = "en", dateFormat = "eu"),
        )
        // The profile fetch must observe the token already in the session — the
        // AuthInterceptor reads bearerToken() synchronously, so if the token is
        // persisted only after currentUser() the request goes out unauthenticated
        // and the backend answers 401 "Authorization token required".
        var tokenSeenDuringProfileFetch: String? = null
        coEvery { h.apiClient.currentUser() } answers {
            tokenSeenDuringProfileFetch = h.sessionManager.bearerToken()
            Result.success(UserProfile(id = 7, username = "alice"))
        }

        val result = h.repository.login("alice", "secret")

        assertTrue(result.isSuccess)
        assertEquals("jwt-123", tokenSeenDuringProfileFetch)
    }

    @Test
    fun `login propagates failure when server rejects credentials`() = runTest {
        val h = Harness()
        coEvery { h.apiClient.login(any(), any()) } returns Result.failure(
            ApiError.Client(401, "Invalid credentials"),
        )

        val result = h.repository.login("alice", "wrong")

        assertTrue(result.isFailure)
        assertFalse(h.sessionManager.observeSession().first().isLoggedIn)
    }

    @Test
    fun `login fails when no token cookie is returned`() = runTest {
        val h = Harness()
        coEvery { h.apiClient.login(any(), any()) } returns Result.success(
            LoginResult(token = null, language = "en", dateFormat = "eu"),
        )

        val result = h.repository.login("alice", "secret")

        assertTrue(result.isFailure)
        assertFalse(h.sessionManager.observeSession().first().isLoggedIn)
    }

    @Test
    fun `loginWithApiToken stores the token and fetches profile`() = runTest {
        val h = Harness()
        coEvery { h.apiClient.currentUser() } returns Result.success(
            UserProfile(id = 3, username = "bot", isAdmin = false),
        )

        val result = h.repository.loginWithApiToken("mycorrhizal_abc123")

        assertTrue(result.isSuccess)
        assertEquals("mycorrhizal_abc123", h.tokenStorage.stored)
        assertTrue(h.sessionManager.observeSession().first().isLoggedIn)
    }

    @Test
    fun `logout clears the session`() = runTest {
        val h = Harness()
        coEvery { h.apiClient.login(any(), any()) } returns Result.success(
            LoginResult(token = "jwt-123", language = null, dateFormat = null),
        )
        coEvery { h.apiClient.currentUser() } returns Result.success(UserProfile(id = 1))
        h.repository.login("alice", "secret")

        h.repository.logout()

        assertEquals(null, h.tokenStorage.stored)
        assertFalse(h.sessionManager.observeSession().first().isLoggedIn)
    }

    // --- M25 ---

    @Test
    fun `updateLanguage patches the server and merges the language into the session`() = runTest {
        val h = Harness()
        h.sessionManager.setSession("https://crm.example.com", "jwt", SessionState(language = "en"))
        coEvery { h.apiClient.updateLanguage("de") } returns Result.success(
            com.mycorrhizal.crm.model.network.MessageResponse(message = "Language updated successfully"),
        )

        val result = h.repository.updateLanguage("de")

        assertTrue(result.isSuccess)
        coVerify { h.apiClient.updateLanguage("de") }
        assertEquals("de", h.sessionManager.observeSession().first().language)
    }

    @Test
    fun `updateLanguage propagates a server rejection`() = runTest {
        val h = Harness()
        coEvery { h.apiClient.updateLanguage("xx") } returns Result.failure(
            ApiError.Client(400, "Invalid value for field 'language'"),
        )

        val result = h.repository.updateLanguage("xx")

        assertTrue(result.isFailure)
        assertEquals("Invalid value for field 'language'", (result.exceptionOrNull() as ApiError).displayMessage)
    }

    @Test
    fun `updateDateFormat patches the server and merges the format into the session`() = runTest {
        val h = Harness()
        h.sessionManager.setSession("https://crm.example.com", "jwt", SessionState(dateFormat = "eu"))
        coEvery { h.apiClient.updateDateFormat("us") } returns Result.success(
            com.mycorrhizal.crm.model.network.MessageResponse(message = "Date format updated successfully"),
        )

        val result = h.repository.updateDateFormat("us")

        assertTrue(result.isSuccess)
        coVerify { h.apiClient.updateDateFormat("us") }
        assertEquals("us", h.sessionManager.observeSession().first().dateFormat)
    }

    @Test
    fun `changePassword delegates to the api client`() = runTest {
        val h = Harness()
        coEvery { h.apiClient.changePassword("old", "new") } returns Result.success(
            com.mycorrhizal.crm.model.network.MessageResponse(message = "Password updated successfully"),
        )

        val result = h.repository.changePassword("old", "new")

        assertTrue(result.isSuccess)
        coVerify { h.apiClient.changePassword("old", "new") }
    }

    @Test
    fun `changePassword surfaces a wrong current password as the server message`() = runTest {
        val h = Harness()
        coEvery { h.apiClient.changePassword("wrong", "new") } returns Result.failure(
            ApiError.Client(400, "Invalid value for field 'current_password'"),
        )

        val result = h.repository.changePassword("wrong", "new")

        assertTrue(result.isFailure)
        assertEquals("Invalid value for field 'current_password'", (result.exceptionOrNull() as ApiError).displayMessage)
    }

    // --- M26: registration + password reset ---

    @Test
    fun `register delegates to the api client and maps success to Unit`() = runTest {
        val h = Harness()
        coEvery { h.apiClient.register("alice", "a@x.com", "hunter2hunter2") } returns Result.success(
            com.mycorrhizal.crm.model.network.MessageResponse(message = "User registered successfully"),
        )

        val result = h.repository.register("alice", "a@x.com", "hunter2hunter2")

        assertTrue(result.isSuccess)
        coVerify { h.apiClient.register("alice", "a@x.com", "hunter2hunter2") }
    }

    @Test
    fun `register propagates a duplicate-account rejection`() = runTest {
        val h = Harness()
        coEvery { h.apiClient.register(any(), any(), any()) } returns Result.failure(
            ApiError.Client(409, "User already exists"),
        )

        val result = h.repository.register("alice", "a@x.com", "hunter2hunter2")

        assertTrue(result.isFailure)
        assertEquals("User already exists", (result.exceptionOrNull() as ApiError).displayMessage)
    }

    @Test
    fun `checkPasswordStrength returns the parsed verdict`() = runTest {
        val h = Harness()
        coEvery { h.apiClient.checkPasswordStrength("short") } returns Result.success(
            com.mycorrhizal.crm.model.network.PasswordStrength(isValid = false, score = 1, feedback = "too weak"),
        )

        val result = h.repository.checkPasswordStrength("short")

        assertTrue(result.isSuccess)
        assertEquals(false, result.getOrThrow().isValid)
    }

    @Test
    fun `requestPasswordReset returns the server message`() = runTest {
        val h = Harness()
        coEvery { h.apiClient.requestPasswordReset("a@x.com") } returns Result.success(
            com.mycorrhizal.crm.model.network.MessageResponse(message = "If an account exists, password reset instructions were sent"),
        )

        val result = h.repository.requestPasswordReset("a@x.com")

        assertTrue(result.isSuccess)
        assertEquals("If an account exists, password reset instructions were sent", result.getOrThrow())
    }

    @Test
    fun `confirmPasswordReset delegates to the api client`() = runTest {
        val h = Harness()
        coEvery { h.apiClient.confirmPasswordReset("token-1", "newpass123") } returns Result.success(
            com.mycorrhizal.crm.model.network.MessageResponse(message = "Password reset successful"),
        )

        val result = h.repository.confirmPasswordReset("token-1", "newpass123")

        assertTrue(result.isSuccess)
        coVerify { h.apiClient.confirmPasswordReset("token-1", "newpass123") }
    }

    // --- N8 two-factor login (issue #814) ---

    @Test
    fun `login on a 2fa account returns TwoFactorRequired and never establishes a session`() = runTest {
        val h = Harness()
        h.sessionManager.setServerUrl("https://crm.example.com")
        coEvery { h.apiClient.login("alice", "secret") } returns Result.success(
            LoginResult(token = null, language = null, dateFormat = null, twoFactorRequired = true, pending2faCookie = "challenge-jwt"),
        )
        coEvery { h.apiClient.currentUser() } returns Result.success(UserProfile(id = 7))

        val result = h.repository.login("alice", "secret")

        assertTrue(result.isSuccess)
        assertEquals(LoginOutcome.TwoFactorRequired, result.getOrThrow())
        // No session, and the profile was never fetched (there is no token yet).
        assertFalse(h.sessionManager.observeSession().first().isLoggedIn)
        coVerify(exactly = 0) { h.apiClient.currentUser() }
        assertNull(h.tokenStorage.stored)
    }

    @Test
    fun `complete2faLogin exchanges the pending challenge for a session`() = runTest {
        val h = Harness()
        h.sessionManager.setServerUrl("https://crm.example.com")
        coEvery { h.apiClient.login(any(), any()) } returns Result.success(
            LoginResult(token = null, language = null, dateFormat = null, twoFactorRequired = true, pending2faCookie = "challenge-jwt"),
        )
        coEvery { h.apiClient.complete2faLogin("123456", "challenge-jwt") } returns Result.success(
            LoginResult(token = "jwt-2fa", language = "en", dateFormat = "eu"),
        )
        coEvery { h.apiClient.currentUser() } returns Result.success(
            UserProfile(id = 7, username = "alice", isAdmin = true, language = "en"),
        )

        val first = h.repository.login("alice", "secret")
        assertEquals(LoginOutcome.TwoFactorRequired, first.getOrThrow())

        val result = h.repository.complete2faLogin("123456")

        assertTrue(result.isSuccess)
        assertEquals("jwt-2fa", h.tokenStorage.stored)
        assertEquals("jwt-2fa", h.sessionManager.bearerToken())
        val state = h.sessionManager.observeSession().first()
        assertTrue(state.isLoggedIn)
        assertEquals("alice", state.username)
    }

    @Test
    fun `complete2faLogin without an in-flight challenge fails fast with 401`() = runTest {
        val h = Harness()

        val result = h.repository.complete2faLogin("123456")

        assertTrue(result.isFailure)
        val error = result.exceptionOrNull() as ApiError
        assertTrue(error is ApiError.Client)
        assertEquals(401, (error as ApiError.Client).code)
        coVerify(exactly = 0) { h.apiClient.complete2faLogin(any(), any()) }
    }

    @Test
    fun `complete2faLogin keeps the challenge on a wrong code so the user can retry`() = runTest {
        val h = Harness()
        coEvery { h.apiClient.login(any(), any()) } returns Result.success(
            LoginResult(token = null, language = null, dateFormat = null, twoFactorRequired = true, pending2faCookie = "challenge-jwt"),
        )
        coEvery { h.apiClient.complete2faLogin("000000", "challenge-jwt") } returns Result.failure(
            ApiError.Client(400, "Invalid value for field 'code'"),
        )
        coEvery { h.apiClient.complete2faLogin("123456", "challenge-jwt") } returns Result.success(
            LoginResult(token = "jwt-2fa", language = null, dateFormat = null),
        )
        coEvery { h.apiClient.currentUser() } returns Result.success(UserProfile(id = 1))

        h.repository.login("alice", "secret")

        val wrong = h.repository.complete2faLogin("000000")
        assertTrue(wrong.isFailure)
        assertEquals(400, ((wrong.exceptionOrNull() as ApiError) as ApiError.Client).code)

        // The same pending challenge is still valid for a retry.
        val retry = h.repository.complete2faLogin("123456")
        assertTrue(retry.isSuccess)
    }

    @Test
    fun `an expired challenge clears the pending state so a retry restarts at step 1`() = runTest {
        val h = Harness()
        coEvery { h.apiClient.login(any(), any()) } returns Result.success(
            LoginResult(token = null, language = null, dateFormat = null, twoFactorRequired = true, pending2faCookie = "challenge-jwt"),
        )
        coEvery { h.apiClient.complete2faLogin("123456", "challenge-jwt") } returns Result.failure(
            ApiError.Client(401, "Invalid or expired two-factor session. Please sign in again."),
        )

        h.repository.login("alice", "secret")

        val expired = h.repository.complete2faLogin("123456")
        assertTrue(expired.isFailure)
        assertEquals(401, ((expired.exceptionOrNull() as ApiError) as ApiError.Client).code)

        // The consumed challenge is gone: a second attempt must NOT hit the
        // server with the old cookie — it restarts at step 1.
        val retry = h.repository.complete2faLogin("654321")
        assertTrue(retry.isFailure)
        assertEquals(401, ((retry.exceptionOrNull() as ApiError) as ApiError.Client).code)
        coVerify(exactly = 1) { h.apiClient.complete2faLogin(any(), any()) }
    }

    // --- N8 two-factor management (issue #814) ---

    @Test
    fun `confirmTwoFactor enables 2fa, returns the recovery codes and keeps the reissued token`() = runTest {
        val h = Harness()
        h.sessionManager.setSession("https://crm.example.com", "old-jwt", SessionState(isLoggedIn = true))
        coEvery { h.apiClient.confirmTwoFactor("123456") } returns Result.success(
            ReissuedTokenResult(
                value = TwoFactorConfirmResponse(
                    message = "Two-factor authentication enabled",
                    recoveryCodes = listOf("ABCDE-FGHIJ-KLMNO"),
                ),
                reissuedToken = "reissued-jwt",
            ),
        )

        val result = h.repository.confirmTwoFactor("123456")

        assertTrue(result.isSuccess)
        assertEquals(listOf("ABCDE-FGHIJ-KLMNO"), result.getOrThrow().recoveryCodes)
        // token_version was bumped — the re-issued token replaced the old JWT so
        // this session survives the mutation.
        assertEquals("reissued-jwt", h.sessionManager.bearerToken())
        assertEquals("reissued-jwt", h.tokenStorage.stored)
        assertTrue(h.sessionManager.observeSession().first().isLoggedIn)
    }

    @Test
    fun `confirmTwoFactor propagates an invalid code as Client 400`() = runTest {
        val h = Harness()
        coEvery { h.apiClient.confirmTwoFactor("000000") } returns Result.failure(
            ApiError.Client(400, "Invalid value for field 'code'"),
        )

        val result = h.repository.confirmTwoFactor("000000")

        assertTrue(result.isFailure)
        val error = result.exceptionOrNull() as ApiError
        assertTrue(error is ApiError.Client)
        assertEquals(400, (error as ApiError.Client).code)
    }

    @Test
    fun `disableTwoFactor keeps the reissued token and reports the server message`() = runTest {
        val h = Harness()
        h.sessionManager.setSession("https://crm.example.com", "old-jwt", SessionState(isLoggedIn = true))
        coEvery { h.apiClient.disableTwoFactor("654321") } returns Result.success(
            ReissuedTokenResult(value = MessageResponse(message = "Two-factor authentication disabled"), reissuedToken = "reissued-jwt-2"),
        )

        val result = h.repository.disableTwoFactor("654321")

        assertTrue(result.isSuccess)
        assertEquals("Two-factor authentication disabled", result.getOrThrow().message)
        assertEquals("reissued-jwt-2", h.sessionManager.bearerToken())
    }

    @Test
    fun `setupTwoFactor surfaces the oidc 403 message for display`() = runTest {
        val h = Harness()
        coEvery { h.apiClient.setupTwoFactor() } returns Result.failure(
            ApiError.Client(403, "Two-factor authentication is unavailable for accounts that sign in with SSO"),
        )

        val result = h.repository.setupTwoFactor()

        assertTrue(result.isFailure)
        val error = result.exceptionOrNull() as ApiError
        assertEquals(403, (error as ApiError.Client).code)
    }

    @Test
    fun `regenerateRecoveryCodes replaces the codes`() = runTest {
        val h = Harness()
        coEvery { h.apiClient.regenerateRecoveryCodes("123456") } returns Result.success(
            ReissuedTokenResult(value = TwoFactorConfirmResponse(recoveryCodes = listOf("NEW-A")), reissuedToken = null),
        )

        val result = h.repository.regenerateRecoveryCodes("123456")

        assertTrue(result.isSuccess)
        assertEquals(listOf("NEW-A"), result.getOrThrow().recoveryCodes)
    }

    @Test
    fun `getTwoFactorStatus reports the enabled flag`() = runTest {
        val h = Harness()
        coEvery { h.apiClient.getTwoFactorStatus() } returns Result.success(
            com.mycorrhizal.crm.model.network.TwoFactorStatusResponse(enabled = true),
        )

        val result = h.repository.getTwoFactorStatus()

        assertTrue(result.isSuccess)
        assertTrue(result.getOrThrow().enabled)
    }
}
