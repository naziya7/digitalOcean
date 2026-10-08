package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds runtime settings loaded from environment variables.
type Config struct {
	HTTPPort           string
	MongoURI           string
	MongoDatabase      string
	DataDir            string
	MaxUploadBytes     int64
	MaxFilesPerRequest int
	MaxImagePixels     int64
	MaxCustomDimension int
}

// Load reads configuration from the environment and applies defaults.
func Load() (Config, error) {
	cfg := Config{
		HTTPPort:           getEnv("HTTP_PORT", "8080"),
		MongoURI:           getEnv("MONGO_URI", "mongodb://localhost:27017"),
		MongoDatabase:      getEnv("MONGO_DATABASE", "thumbnails"),
		DataDir:            getEnv("DATA_DIR", "./data"),
		MaxUploadBytes:     getEnvInt64("MAX_UPLOAD_BYTES", 10<<20), // 10 MiB
		MaxFilesPerRequest: getEnvInt("MAX_FILES_PER_REQUEST", 5),
		MaxImagePixels:     getEnvInt64("MAX_IMAGE_PIXELS", 25_000_000),
		MaxCustomDimension: getEnvInt("MAX_CUSTOM_DIMENSION", 2000),
	}

	if cfg.MongoURI == "" {
		return Config{}, fmt.Errorf("MONGO_URI is required")
	}
	if cfg.MongoDatabase == "" {
		return Config{}, fmt.Errorf("MONGO_DATABASE is required")
	}
	if cfg.DataDir == "" {
		return Config{}, fmt.Errorf("DATA_DIR is required")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func getEnvInt64(key string, fallback int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}
