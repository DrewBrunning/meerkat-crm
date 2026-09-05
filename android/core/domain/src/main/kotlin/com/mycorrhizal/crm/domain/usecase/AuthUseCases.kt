package com.mycorrhizal.crm.domain.usecase

import com.mycorrhizal.crm.domain.repository.AuthRepository
import com.mycorrhizal.crm.domain.repository.LoginOutcome
import javax.inject.Inject

/**
 * Logs the user in. Field-level validation (blank identifier/password) is a
 * UI concern and lives in the ViewModel, where user-facing localized strings
 * can be produced; this use case only performs the credential exchange and
 * reports the outcome.
 *
 * [Result.TwoFactorRequired] reports the account has 2FA enabled and the login
 * must continue via [Complete2faLoginUseCase] before any session exists.
 */
class LoginUseCase @Inject constructor(private val authRepository: AuthRepository) {
    sealed interface Result {
        data object Success : Result
        data object TwoFactorRequired : Result
        data class Failure(val message: String) : Result
    }

    suspend operator fun invoke(identifier: String, password: String): Result {
        val outcome = authRepository.login(identifier.trim(), password)
        return if (outcome.isFailure) {
            Result.Failure(outcome.exceptionOrNull()?.message ?: "Login failed")
        } else {
            when (outcome.getOrThrow()) {
                LoginOutcome.SessionEstablished -> Result.Success
                LoginOutcome.TwoFactorRequired -> Result.TwoFactorRequired
            }
        }
    }
}

/** Authenticates with a `mycorrhizal_` API token. */
class LoginWithApiTokenUseCase @Inject constructor(private val authRepository: AuthRepository) {
    sealed interface Result {
        data object Success : Result
        data class Failure(val message: String) : Result
    }

    suspend operator fun invoke(token: String): Result {
        val outcome = authRepository.loginWithApiToken(token.trim())
        return if (outcome.isSuccess) {
            Result.Success
        } else {
            Result.Failure(outcome.exceptionOrNull()?.message ?: "Login failed")
        }
    }
}
