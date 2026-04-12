package config

import (
	"bufio"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var loadEnvOnce sync.Once

func LoadEnv() {
	loadEnvOnce.Do(func() {
		loadDotEnv(".env")
		if env := strings.TrimSpace(os.Getenv("APP_ENV")); env != "" {
			loadDotEnv(".env." + env)
		}
	})
}

func GetAIAPIKey() string {
	LoadEnv()
	return firstEnv("AI_API_KEY", "OPENAI_API_KEY")
}

func GetAIBaseURL() string {
	LoadEnv()
	return firstEnv("AI_BASE_URL", "OPENAI_BASE_URL", "BASE_URL")
}

func GetAIModelID() string {
	LoadEnv()
	return firstEnv("AI_MODEL_ID", "OPENAI_MODEL")
}

func FirstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func loadDotEnv(name string) {
	path := filepath.Clean(name)
	file, err := os.Open(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[Config] load %s failed: %v", path, err)
		}
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}

		value = strings.TrimSpace(value)
		value = strings.Trim(value, "\"")
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, value)
	}

	if err := scanner.Err(); err != nil {
		log.Printf("[Config] scan %s failed: %v", path, err)
	}
}
