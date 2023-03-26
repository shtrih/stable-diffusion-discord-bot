package statistics

import (
	"context"

	"stable_diffusion_bot/entities"
)

type Repository interface {
	AddProcessingTime(ctx context.Context, stat *entities.Statistics) (int64, error)
	GetStatByMember(ctx context.Context, memberID string) (*entities.StatsByMember, error)
}
