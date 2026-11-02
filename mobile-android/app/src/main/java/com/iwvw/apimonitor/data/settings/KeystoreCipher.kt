package com.iwvw.apimonitor.data.settings

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

internal class KeystoreCipher {
    private val keyStore: KeyStore = KeyStore.getInstance(ANDROID_KEYSTORE).apply { load(null) }

    fun encrypt(plain: String): String? {
        if (plain.isEmpty()) return null
        return runCatching {
            val cipher = Cipher.getInstance(TRANSFORMATION)
            cipher.init(Cipher.ENCRYPT_MODE, requireKey())
            val iv = cipher.iv
            val payload = cipher.doFinal(plain.toByteArray(Charsets.UTF_8))
            val packed = ByteArray(1 + iv.size + payload.size)
            packed[0] = iv.size.toByte()
            iv.copyInto(packed, 1)
            payload.copyInto(packed, 1 + iv.size)
            Base64.encodeToString(packed, Base64.NO_WRAP)
        }.getOrNull()
    }

    fun decrypt(encoded: String): String? {
        if (encoded.isEmpty()) return null
        return runCatching {
            val packed = Base64.decode(encoded, Base64.NO_WRAP)
            val ivSize = packed[0].toInt()
            val iv = packed.copyOfRange(1, 1 + ivSize)
            val payload = packed.copyOfRange(1 + ivSize, packed.size)
            val cipher = Cipher.getInstance(TRANSFORMATION)
            cipher.init(Cipher.DECRYPT_MODE, requireKey(), GCMParameterSpec(TAG_BITS, iv))
            String(cipher.doFinal(payload), Charsets.UTF_8)
        }.getOrNull()
    }

    private fun requireKey(): SecretKey {
        (keyStore.getEntry(KEY_ALIAS, null) as? KeyStore.SecretKeyEntry)?.let { return it.secretKey }
        val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, ANDROID_KEYSTORE)
        generator.init(
            KeyGenParameterSpec.Builder(
                KEY_ALIAS,
                KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT,
            )
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                .build(),
        )
        return generator.generateKey()
    }

    companion object {
        private const val ANDROID_KEYSTORE = "AndroidKeyStore"
        private const val KEY_ALIAS = "apimonitor_settings_key"
        private const val TRANSFORMATION = "AES/GCM/NoPadding"
        private const val TAG_BITS = 128
    }
}
