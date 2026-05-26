package testenv

import (
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

func init() {
	_ = godotenv.Load(".env")
	if _, err := os.Stat(".env"); err != nil {
		_ = godotenv.Load(filepath.Join("..", "..", ".env"))
	}
}
