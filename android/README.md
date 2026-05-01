# PUMP Android App

Android client for the PUMP workout diary. Connects to any PUMP API server.

## Requirements

- Android 16 (API 36) or later
- JDK 21

## Setup

First-time contributors must bootstrap the Gradle wrapper (the `gradle-wrapper.jar` binary is not committed):

```bash
cd android
gradle wrapper --gradle-version 8.11.1
chmod +x gradlew
```

## Build

```bash
cd android
./gradlew assembleDebug
```

The APK will be at `app/build/outputs/apk/debug/app-debug.apk`.

## Configuration

On first launch, open the Settings tab and configure:

| Field   | Description                                              |
|---------|----------------------------------------------------------|
| API URL | Base URL of your PUMP server (e.g. `http://192.168.1.10:8080`) |
| API Key | Must match the `API_KEY` env var on the server (optional) |
