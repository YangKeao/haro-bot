package memory

import (
	"context"

	"github.com/YangKeao/haro-bot/internal/config"
)

type VectorStore interface {
	EnsureSchema(ctx context.Context, cfg config.MemoryConfig) error
	Insert(ctx context.Context, item MemoryItem, vector []float32) (int64, error)
	Update(ctx context.Context, item MemoryItem, vector []float32) error
	Delete(ctx context.Context, id int64) error
	Search(ctx context.Context, userID int64, sessionID *int64, vector []float32, limit int) ([]MemoryItem, error)
}
