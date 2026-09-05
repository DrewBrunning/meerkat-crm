package com.mycorrhizal.crm.domain.usecase

import com.mycorrhizal.crm.domain.repository.AuthRepository
import com.mycorrhizal.crm.domain.repository.LoginOutcome
import io.mockk.coEvery
import io.mockk.coVerify
import io.mockk.mockk
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class LoginUseCaseTest {

    private val authRepository = mockk<AuthRepository>()
    private val useCase = LoginUseCase(authRepository)

    @Test
    fun `trims the identifier and password before delegating`() = runTest {
        coEvery { authRepository.login("alice", "secret") } returns
            Result.success(LoginOutcome.SessionEstablished)

        val result = useCase(" alice ", "secret")

        assertTrue(result is LoginUseCase.Result.Success)
        coVerify { authRepository.login("alice", "secret") }
    }

    @Test
    fun `a 2fa account surfaces TwoFactorRequired instead of a session`() = runTest {
        coEvery { authRepository.login("alice", "secret") } returns
            Result.success(LoginOutcome.TwoFactorRequired)

        val result = useCase("alice", "secret")

        assertTrue(result is LoginUseCase.Result.TwoFactorRequired)
    }

    @Test
    fun `failure surfaces the repository's message`() = runTest {
        coEvery { authRepository.login("alice", "wrong") } returns
            Result.failure(RuntimeException("invalid credentials"))

        val result = useCase("alice", "wrong")

        assertTrue(result is LoginUseCase.Result.Failure)
        assertEquals("invalid credentials", (result as LoginUseCase.Result.Failure).message)
    }

    @Test
    fun `failure with no message falls back to a generic message`() = runTest {
        coEvery { authRepository.login("alice", "wrong") } returns Result.failure(RuntimeException())

        val result = useCase("alice", "wrong")

        assertTrue(result is LoginUseCase.Result.Failure)
        assertEquals("Login failed", (result as LoginUseCase.Result.Failure).message)
    }
}

class LoginWithApiTokenUseCaseTest {

    private val authRepository = mockk<AuthRepository>()
    private val useCase = LoginWithApiTokenUseCase(authRepository)

    @Test
    fun `trims the token before delegating`() = runTest {
        coEvery { authRepository.loginWithApiToken("mycorrhizal_abc123") } returns Result.success(Unit)

        val result = useCase(" mycorrhizal_abc123 ")

        assertTrue(result is LoginWithApiTokenUseCase.Result.Success)
        coVerify { authRepository.loginWithApiToken("mycorrhizal_abc123") }
    }

    @Test
    fun `failure surfaces the repository's message`() = runTest {
        coEvery { authRepository.loginWithApiToken("bad-token") } returns
            Result.failure(RuntimeException("token revoked"))

        val result = useCase("bad-token")

        assertTrue(result is LoginWithApiTokenUseCase.Result.Failure)
        assertEquals("token revoked", (result as LoginWithApiTokenUseCase.Result.Failure).message)
    }

    @Test
    fun `failure with no message falls back to a generic message`() = runTest {
        coEvery { authRepository.loginWithApiToken("bad-token") } returns Result.failure(RuntimeException())

        val result = useCase("bad-token")

        assertTrue(result is LoginWithApiTokenUseCase.Result.Failure)
        assertEquals("Login failed", (result as LoginWithApiTokenUseCase.Result.Failure).message)
    }
}
