package com.rwlove.pump.data.api

import retrofit2.Response
import retrofit2.http.Body
import retrofit2.http.DELETE
import retrofit2.http.GET
import retrofit2.http.PATCH
import retrofit2.http.POST
import retrofit2.http.PUT
import retrofit2.http.Path

interface PumpApiService {
    @GET("api/exercises")
    suspend fun getExercises(): List<ExerciseDto>

    @POST("api/exercises")
    suspend fun createExercise(@Body ex: ExerciseDto): Response<Unit>

    @PUT("api/exercises/{id}")
    suspend fun updateExercise(@Path("id") id: Int, @Body ex: ExerciseDto): Response<Unit>

    @PATCH("api/exercises/{id}/color")
    suspend fun patchColor(@Path("id") id: Int, @Body body: ColorBody): Response<Unit>

    @DELETE("api/exercises/{id}")
    suspend fun deleteExercise(@Path("id") id: Int): Response<Unit>

    @GET("api/sets")
    suspend fun getSets(): List<SetDto>

    @PUT("api/sets/date/{date}")
    suspend fun putSetsByDate(@Path("date") date: String, @Body sets: List<SetDto>): Response<Unit>

    @GET("api/weight")
    suspend fun getWeight(): List<BodyWeightDto>

    @POST("api/weight")
    suspend fun createWeight(@Body w: BodyWeightDto): Response<Unit>

    @DELETE("api/weight/{id}")
    suspend fun deleteWeight(@Path("id") id: Int): Response<Unit>

    @GET("api/config")
    suspend fun getConfig(): ConfigDto

    @PUT("api/config")
    suspend fun putConfig(@Body cfg: ConfigDto): Response<Unit>
}
