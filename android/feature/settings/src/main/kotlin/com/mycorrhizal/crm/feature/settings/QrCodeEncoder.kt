package com.mycorrhizal.crm.feature.settings

import android.graphics.Bitmap
import android.graphics.Color
import com.google.zxing.BarcodeFormat
import com.google.zxing.EncodeHintType
import com.google.zxing.qrcode.QRCodeWriter

/**
 * Issue #814 Phase 2: renders the TOTP enrollment `otpauth://` URI as a QR
 * bitmap via the pure-Java zxing encoder (no camera/scanner involved — the
 * user points their own authenticator app at it). Returns null for a blank
 * URI or an encoding failure; the caller shows a manual-key fallback anyway,
 * so a missing QR is never blocking.
 */
object QrCodeEncoder {

    fun encode(content: String, sizePx: Int = 512): Bitmap? {
        if (content.isBlank()) return null
        return runCatching {
            val matrix = QRCodeWriter().encode(
                content,
                BarcodeFormat.QR_CODE,
                sizePx,
                sizePx,
                mapOf(EncodeHintType.MARGIN to 1),
            )
            val width = matrix.width
            val height = matrix.height
            val pixels = IntArray(width * height)
            for (y in 0 until height) {
                val rowStart = y * width
                for (x in 0 until width) {
                    pixels[rowStart + x] = if (matrix.get(x, y)) Color.BLACK else Color.WHITE
                }
            }
            Bitmap.createBitmap(width, height, Bitmap.Config.ARGB_8888).apply {
                setPixels(pixels, 0, width, 0, 0, width, height)
            }
        }.getOrNull()
    }
}
