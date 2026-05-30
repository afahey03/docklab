package repositories

import (
	"context"
	"errors"

	"github.com/afahey03/docklab/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrEnvironmentNotFound = errors.New("environment not found")

type EnvironmentRepository interface {
	Create(ctx context.Context, userEmail, name, image, status, containerID string) (*models.Environment, error)
	ListByUserEmail(ctx context.Context, userEmail string) ([]models.Environment, error)
	GetByIDForUser(ctx context.Context, id, userEmail string) (*models.Environment, error)
	UpdateStatus(ctx context.Context, id, userEmail, status string) (*models.Environment, error)
	UpdateProvisioning(ctx context.Context, id, userEmail, cloudStatus, cloudRegion, instanceID, publicIP, terraformDir, cloudError string) (*models.Environment, error)
	Delete(ctx context.Context, id, userEmail string) error
}

type PostgresEnvironmentRepository struct {
	db *pgxpool.Pool
}

func NewPostgresEnvironmentRepository(db *pgxpool.Pool) *PostgresEnvironmentRepository {
	return &PostgresEnvironmentRepository{db: db}
}

func (r *PostgresEnvironmentRepository) Create(ctx context.Context, userEmail, name, image, status, containerID string) (*models.Environment, error) {
	if r.db == nil {
		return nil, errors.New("database connection is nil")
	}

	const query = `
		INSERT INTO environments (user_email, name, image, status, container_id)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, user_email, name, image, status, container_id, cloud_status, cloud_region, instance_id, public_ip, terraform_dir, cloud_error, created_at, updated_at
	`

	var env models.Environment
	err := r.db.QueryRow(ctx, query, userEmail, name, image, status, containerID).Scan(
		&env.ID,
		&env.UserEmail,
		&env.Name,
		&env.Image,
		&env.Status,
		&env.ContainerID,
		&env.CloudStatus,
		&env.CloudRegion,
		&env.InstanceID,
		&env.PublicIP,
		&env.TerraformDir,
		&env.CloudError,
		&env.CreatedAt,
		&env.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	return &env, nil
}

func (r *PostgresEnvironmentRepository) ListByUserEmail(ctx context.Context, userEmail string) ([]models.Environment, error) {
	if r.db == nil {
		return nil, errors.New("database connection is nil")
	}

	const query = `
		SELECT id, user_email, name, image, status, container_id, cloud_status, cloud_region, instance_id, public_ip, terraform_dir, cloud_error, created_at, updated_at
		FROM environments
		WHERE user_email = $1
		ORDER BY created_at DESC
	`

	rows, err := r.db.Query(ctx, query, userEmail)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	environments := make([]models.Environment, 0)
	for rows.Next() {
		var env models.Environment
		if err := rows.Scan(
			&env.ID,
			&env.UserEmail,
			&env.Name,
			&env.Image,
			&env.Status,
			&env.ContainerID,
			&env.CloudStatus,
			&env.CloudRegion,
			&env.InstanceID,
			&env.PublicIP,
			&env.TerraformDir,
			&env.CloudError,
			&env.CreatedAt,
			&env.UpdatedAt,
		); err != nil {
			return nil, err
		}
		environments = append(environments, env)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return environments, nil
}

func (r *PostgresEnvironmentRepository) GetByIDForUser(ctx context.Context, id, userEmail string) (*models.Environment, error) {
	if r.db == nil {
		return nil, errors.New("database connection is nil")
	}

	const query = `
		SELECT id, user_email, name, image, status, container_id, cloud_status, cloud_region, instance_id, public_ip, terraform_dir, cloud_error, created_at, updated_at
		FROM environments
		WHERE id = $1 AND user_email = $2
	`

	var env models.Environment
	err := r.db.QueryRow(ctx, query, id, userEmail).Scan(
		&env.ID,
		&env.UserEmail,
		&env.Name,
		&env.Image,
		&env.Status,
		&env.ContainerID,
		&env.CloudStatus,
		&env.CloudRegion,
		&env.InstanceID,
		&env.PublicIP,
		&env.TerraformDir,
		&env.CloudError,
		&env.CreatedAt,
		&env.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrEnvironmentNotFound
	}
	if err != nil {
		return nil, err
	}

	return &env, nil
}

func (r *PostgresEnvironmentRepository) UpdateStatus(ctx context.Context, id, userEmail, status string) (*models.Environment, error) {
	if r.db == nil {
		return nil, errors.New("database connection is nil")
	}

	const query = `
		UPDATE environments
		SET status = $3, updated_at = NOW()
		WHERE id = $1 AND user_email = $2
		RETURNING id, user_email, name, image, status, container_id, cloud_status, cloud_region, instance_id, public_ip, terraform_dir, cloud_error, created_at, updated_at
	`

	var env models.Environment
	err := r.db.QueryRow(ctx, query, id, userEmail, status).Scan(
		&env.ID,
		&env.UserEmail,
		&env.Name,
		&env.Image,
		&env.Status,
		&env.ContainerID,
		&env.CloudStatus,
		&env.CloudRegion,
		&env.InstanceID,
		&env.PublicIP,
		&env.TerraformDir,
		&env.CloudError,
		&env.CreatedAt,
		&env.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrEnvironmentNotFound
	}
	if err != nil {
		return nil, err
	}

	return &env, nil
}

func (r *PostgresEnvironmentRepository) UpdateProvisioning(ctx context.Context, id, userEmail, cloudStatus, cloudRegion, instanceID, publicIP, terraformDir, cloudError string) (*models.Environment, error) {
	if r.db == nil {
		return nil, errors.New("database connection is nil")
	}

	const query = `
		UPDATE environments
		SET
			cloud_status = $3,
			cloud_region = $4,
			instance_id = $5,
			public_ip = $6,
			terraform_dir = $7,
			cloud_error = $8,
			updated_at = NOW()
		WHERE id = $1 AND user_email = $2
		RETURNING id, user_email, name, image, status, container_id, cloud_status, cloud_region, instance_id, public_ip, terraform_dir, cloud_error, created_at, updated_at
	`

	var env models.Environment
	err := r.db.QueryRow(ctx, query, id, userEmail, cloudStatus, cloudRegion, instanceID, publicIP, terraformDir, cloudError).Scan(
		&env.ID,
		&env.UserEmail,
		&env.Name,
		&env.Image,
		&env.Status,
		&env.ContainerID,
		&env.CloudStatus,
		&env.CloudRegion,
		&env.InstanceID,
		&env.PublicIP,
		&env.TerraformDir,
		&env.CloudError,
		&env.CreatedAt,
		&env.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrEnvironmentNotFound
	}
	if err != nil {
		return nil, err
	}

	return &env, nil
}

func (r *PostgresEnvironmentRepository) Delete(ctx context.Context, id, userEmail string) error {
	if r.db == nil {
		return errors.New("database connection is nil")
	}

	const query = `
		DELETE FROM environments
		WHERE id = $1 AND user_email = $2
	`

	result, err := r.db.Exec(ctx, query, id, userEmail)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrEnvironmentNotFound
	}

	return nil
}
