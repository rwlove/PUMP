package com.rwlove.pump.data.repository

import com.rwlove.pump.data.api.BodyWeightDto
import com.rwlove.pump.data.api.ConfigDto
import com.rwlove.pump.data.api.ExerciseDto
import com.rwlove.pump.data.api.PumpApiService
import com.rwlove.pump.data.api.SetDto
import javax.inject.Inject
import javax.inject.Singleton

@Singleton
class PumpRepository @Inject constructor(
    private val apiService: PumpApiService
) {
    /**
     * Hits GET /api/ping to verify the server is reachable and the API key is accepted.
     * Returns [Result.success] on HTTP 200, [Result.failure] with a descriptive message otherwise.
     */
    suspend fun testConnection(): Result<Unit> = runCatching {
        val response = apiService.ping()
        if (!response.isSuccessful) {
            throw Exception(
                when (response.code()) {
                    401 -> "Invalid API key (401 Unauthorized)"
                    403 -> "Access denied (403 Forbidden)"
                    else -> "Server returned HTTP ${response.code()}"
                }
            )
        }
    }

    suspend fun getExercises(): Result<List<ExerciseDto>> = runCatching {
        apiService.getExercises()
    }

    suspend fun createExercise(ex: ExerciseDto): Result<Unit> = runCatching {
        val response = apiService.createExercise(ex)
        if (!response.isSuccessful) {
            throw Exception("Failed to create exercise: ${response.code()}")
        }
    }

    suspend fun updateExercise(id: Int, ex: ExerciseDto): Result<Unit> = runCatching {
        val response = apiService.updateExercise(id, ex)
        if (!response.isSuccessful) {
            throw Exception("Failed to update exercise: ${response.code()}")
        }
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

    suspend fun getConfig(): Result<ConfigDto> = runCatching {
        apiService.getConfig()
    }

    suspend fun putConfig(cfg: ConfigDto): Result<Unit> = runCatching {
        val response = apiService.putConfig(cfg)
        if (!response.isSuccessful) {
            throw Exception("Failed to save config: ${response.code()}")
        }
    }
}
