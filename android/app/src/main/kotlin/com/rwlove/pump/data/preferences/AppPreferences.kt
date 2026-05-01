package com.rwlove.pump.data.preferences

import android.content.Context
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map
import javax.inject.Inject
import javax.inject.Singleton

private val Context.dataStore: DataStore<Preferences> by preferencesDataStore(name = "pump_settings")

@Singleton
class AppPreferences @Inject constructor(
    @ApplicationContext private val context: Context
) {
    companion object {
        private val API_URL_KEY = stringPreferencesKey("api_url")
        private val API_KEY_KEY = stringPreferencesKey("api_key")
        const val DEFAULT_API_URL = "http://localhost:8080"
        const val DEFAULT_API_KEY = ""
    }

    val apiUrl: Flow<String> = context.dataStore.data.map { prefs ->
        prefs[API_URL_KEY] ?: DEFAULT_API_URL
    }

    val apiKey: Flow<String> = context.dataStore.data.map { prefs ->
        prefs[API_KEY_KEY] ?: DEFAULT_API_KEY
    }

    suspend fun setApiUrl(url: String) {
        context.dataStore.edit { prefs ->
            prefs[API_URL_KEY] = url
        }
    }

    suspend fun setApiKey(key: String) {
        context.dataStore.edit { prefs ->
            prefs[API_KEY_KEY] = key
        }
    }
}
