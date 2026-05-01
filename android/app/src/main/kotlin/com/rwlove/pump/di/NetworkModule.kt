package com.rwlove.pump.di

import com.rwlove.pump.data.api.PumpApiService
import com.rwlove.pump.data.preferences.AppPreferences
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import okhttp3.Interceptor
import okhttp3.OkHttpClient
import okhttp3.logging.HttpLoggingInterceptor
import retrofit2.Retrofit
import retrofit2.converter.gson.GsonConverterFactory
import javax.inject.Singleton

@Module
@InstallIn(SingletonComponent::class)
object NetworkModule {

    @Provides
    @Singleton
    fun provideOkHttpClient(appPreferences: AppPreferences): OkHttpClient {
        val logging = HttpLoggingInterceptor().apply {
            level = HttpLoggingInterceptor.Level.BODY
        }

        val apiKeyInterceptor = Interceptor { chain ->
            val apiKey = runBlocking { appPreferences.apiKey.first() }
            val request = if (apiKey.isNotEmpty()) {
                chain.request().newBuilder()
                    .addHeader("X-Api-Key", apiKey)
                    .build()
            } else {
                chain.request()
            }
            chain.proceed(request)
        }

        // Dynamic base URL interceptor: rewrites the host/port/scheme to match
        // the user's configured API URL at call time.
        val dynamicBaseUrlInterceptor = Interceptor { chain ->
            val currentUrl = runBlocking { appPreferences.apiUrl.first() }
            val originalRequest = chain.request()
            val originalUrl = originalRequest.url

            val baseUrl = okhttp3.HttpUrl.parse(currentUrl.trimEnd('/'))
            if (baseUrl != null) {
                val newUrl = originalUrl.newBuilder()
                    .scheme(baseUrl.scheme())
                    .host(baseUrl.host())
                    .port(baseUrl.port())
                    .build()
                val newRequest = originalRequest.newBuilder().url(newUrl).build()
                chain.proceed(newRequest)
            } else {
                chain.proceed(originalRequest)
            }
        }

        return OkHttpClient.Builder()
            .addInterceptor(dynamicBaseUrlInterceptor)
            .addInterceptor(apiKeyInterceptor)
            .addInterceptor(logging)
            .build()
    }

    @Provides
    @Singleton
    fun provideRetrofit(okHttpClient: OkHttpClient): Retrofit {
        // The base URL here is a placeholder; the dynamic interceptor overrides it.
        return Retrofit.Builder()
            .baseUrl("http://localhost/")
            .client(okHttpClient)
            .addConverterFactory(GsonConverterFactory.create())
            .build()
    }

    @Provides
    @Singleton
    fun providePumpApiService(retrofit: Retrofit): PumpApiService {
        return retrofit.create(PumpApiService::class.java)
    }
}
