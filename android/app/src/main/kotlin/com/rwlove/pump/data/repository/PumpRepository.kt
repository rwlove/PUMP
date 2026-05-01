package com.rwlove.pump.data.repository

import com.rwlove.pump.data.api.BodyWeightDto
import com.rwlove.pump.data.api.ExerciseDto
import com.rwlove.pump.data.api.PumpApiService
import com.rwlove.pump.data.api.SetDto
import javax.inject.Inject
import javax.inject.Singleton

@Singleton
class PumpRepository @Inject constructor(
    private val apiService: PumpApiService
) {
    suspend fun getExercises(): Result<List<ExerciseDto>> = runCatching {
        apiService.getExercises()
    }

    suspend fun getSets(): Result<List<SetDto>> = runCatching {
        apiService.getSets()
    }

    suspend fun putSetsByDate(date: String, sets: List<SetDto>): Result<Unit> = runCatching {
        val response = apiService.putSetsByDate(date, sets)
        if (!response.isSuccessful) {
            throw Exception("Failed to save sets: ${response.code()}")
        }
    }

    suspend fun deleteExercise(id: Int): Result<Unit> = runCatching {
        val response = apiService.deleteExercise(id)
        if (!response.isSuccessful) {
            throw Exception("Failed to delete exercise: ${response.code()}")
        }
    }

    suspend fun getWeight(): Result<List<BodyWeightDto>> = runCatching {
        apiService.getWeight()
    }

    suspend fun postWeight(w: BodyWeightDto): Result<Unit> = runCatching {
        val response = apiService.createWeight(w)
        if (!response.isSuccessful) {
            throw Exception("Failed to create weight entry: ${response.code()}")
        }
    }

    suspend fun deleteWeight(id: Int): Result<Unit> = runCatching {
        val response = apiService.deleteWeight(id)
        if (!response.isSuccessful) {
            throw Exception("Failed to delete weight entry: ${response.code()}")
        }
    }
}
