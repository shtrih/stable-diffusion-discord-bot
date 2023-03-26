package statistics

import (
	"context"
	"database/sql"
	"errors"

	"stable_diffusion_bot/clock"
	"stable_diffusion_bot/entities"
)

type sqliteRepo struct {
	dbConn *sql.DB
	clock  clock.Clock
}

type Config struct {
	DB *sql.DB
}

func NewRepository(cfg *Config) (Repository, error) {
	if cfg.DB == nil {
		return nil, errors.New("missing DB parameter")
	}

	newRepo := &sqliteRepo{
		dbConn: cfg.DB,
		clock:  clock.NewClock(),
	}

	return newRepo, nil
}

func (repo *sqliteRepo) AddProcessingTime(ctx context.Context, stat *entities.Statistics) (int64, error) {
	stat.CreatedAt = repo.clock.Now()

	res, err := repo.dbConn.ExecContext(ctx, `INSERT INTO statistics (image_generation_id, member_id, time_ms, created_at) VALUES (?,?,?,?)`,
		stat.ImageGenerationID, stat.MemberID, stat.TimeMs, stat.CreatedAt)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (repo *sqliteRepo) GetStatByMember(ctx context.Context, memberID string) (*entities.StatsByMember, error) {
	var result entities.StatsByMember

	err := repo.dbConn.QueryRowContext(ctx, `
SELECT
    IFNULL(member_id, '')   AS member_id,
    IFNULL(COUNT(*), 0)     AS count,
    IFNULL(SUM(time_ms), 0) AS time_ms
FROM statistics WHERE member_id = ?`, memberID).
		Scan(&result.MemberID, &result.Count, &result.TimeMs)
	if err != nil {
		return nil, err
	}

	if result.MemberID == "" {
		return nil, nil
	}

	return &result, nil
}
