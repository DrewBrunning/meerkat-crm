package com.mycorrhizal.crm.domain.repository

/**
 * Full-dataset export (web-parity for the Settings → Data export section).
 * Each method returns the raw file bytes for the format; sensitivity and
 * field-selection defaults are the backend's own (all sections, private/
 * secret excluded) unless a method exposes the explicit override.
 */
interface ExportRepository {
    /** GET /export — the full per-user backup as one CSV file. */
    suspend fun exportDataCsv(): Result<ByteArray>

    /** GET /export/vcf (no vcard_uid) — every contact as one .vcf file. [version] 3 or null → 4.0. */
    suspend fun exportContactsVcf(version: Int?): Result<ByteArray>

    /** GET /export/jscontact — every contact as a JSContact (RFC 9553) JSON array. */
    suspend fun exportContactsJsContact(): Result<ByteArray>

    /** GET /audit/export — the caller's full audit trail as CSV. */
    suspend fun exportAuditLogCsv(): Result<ByteArray>
}
