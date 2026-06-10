package repositories

import (
	"context"
	"errors"

	"github.com/afahey03/docklab/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrSnapshotNotFound = errors.New("snapshot not found")

type SnapshotRepository interface {
	Create(ctx context.Context, environmentID, userEmail, imageTag, note, runtimeTarget string) (*models.EnvironmentSnapshot, error)
	ListForEnvironment(ctx context.Context, environmentID, userEmail string) ([]models.EnvironmentSnapshot, error)
	GetByIDForUser(ctx context.Context, id, userEmail string) (*models.EnvironmentSnapshot, error)
	Delete(ctx context.Context, id, userEmail string) error
}

type PostgresSnapshotRepository struct {
	db *pgxpool.Pool
}

func NewPostgresSnapshotRepository(db *pgxpool.Pool) *PostgresSnapshotRepository {
	return &PostgresSnapshotRepository{db: db}
}

const snapshotColumns = `id, environment_id, user_email, image_tag, note, runtime_target, created_at`

func scanSnapshot(row interface{ Scan(dest ...any) error }, snapshot *models.EnvironmentSnapshot) error {
	return row.Scan(
		&snapshot.ID,
		&snapshot.EnvironmentID,
		&snapshot.UserEmail,
		&snapshot.ImageTag,
		&snapshot.Note,
		&snapshot.RuntimeTarget,
		&snapshot.CreatedAt,
	)
}

func (r *PostgresSnapshotRepository) Create(ctx context.Context, environmentID, userEmail, imageTag, note, runtimeTarget string) (*models.EnvironmentSnapshot, error) {
	if r.db == nil {
		return nil, errors.New("database connection is nil")
	}

	query := `
		INSERT INTO environment_snapshots (environment_id, user_email, image_tag, note, runtime_target)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING ` + snapshotColumns

	var snapshot models.EnvironmentSnapshot
	if err := scanSnapshot(r.db.QueryRow(ctx, query, environmentID, userEmail, imageTag, note, runtimeTarget), &snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func (r *PostgresSnapshotRepository) ListForEnvironment(ctx context.Context, environmentID, userEmail string) ([]models.EnvironmentSnapshot, error) {
	if r.db == nil {
		return nil, errors.New("database connection is nil")
	}

	query := `
		SELECT ` + snapshotColumns + `
		FROM environment_snapshots
		WHERE environment_id = $1 AND user_email = $2
		ORDER BY created_at DESC`

	rows, err := r.db.Query(ctx, query, environmentID, userEmail)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	snapshots := make([]models.EnvironmentSnapshot, 0)
	for rows.Next() {
		var snapshot models.EnvironmentSnapshot
		if err := scanSnapshot(rows, &snapshot); err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return snapshots, nil
}

func (r *PostgresSnapshotRepository) GetByIDForUser(ctx context.Context, id, userEmail string) (*models.EnvironmentSnapshot, error) {
	if r.db == nil {
		return nil, errors.New("database connection is nil")
	}

	query := `
		SELECT ` + snapshotColumns + `
		FROM environment_snapshots
		WHERE id = $1 AND user_email = $2`

	var snapshot models.EnvironmentSnapshot
	err := scanSnapshot(r.db.QueryRow(ctx, query, id, userEmail), &snapshot)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSnapshotNotFound
	}
	if err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func (r *PostgresSnapshotRepository) Delete(ctx context.Context, id, userEmail string) error {
	if r.db == nil {
		return errors.New("database connection is nil")
	}

	result, err := r.db.Exec(ctx, `DELETE FROM environment_snapshots WHERE id = $1 AND user_email = $2`, id, userEmail)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrSnapshotNotFound
	}
	return nil
}
