package repositories

import (
	"context"
	"errors"
	"time"

	"github.com/afahey03/docklab/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrEnvironmentNotFound = errors.New("environment not found")

type EnvironmentRepository interface {
	Create(ctx context.Context, userEmail, name, image, status, containerID, creationMode string) (*models.Environment, error)
	ListByUserEmail(ctx context.Context, userEmail string) ([]models.Environment, error)
	GetByIDForUser(ctx context.Context, id, userEmail string) (*models.Environment, error)
	UpdateStatus(ctx context.Context, id, userEmail, status string) (*models.Environment, error)
	UpdateProvisioning(ctx context.Context, id, userEmail, cloudStatus, cloudRegion, cloudInstanceType, cloudKeyName, instanceID, publicIP, terraformDir, cloudError string, cloudProvisionedAt *time.Time) (*models.Environment, error)
	UpdateRuntime(ctx context.Context, id, userEmail, runtimeTarget, containerID, status string) (*models.Environment, error)
	Delete(ctx context.Context, id, userEmail string) error

	// Activity tracking
	UpdateLastActivity(ctx context.Context, id string) error
	ListRunningIdleSince(ctx context.Context, since time.Time) ([]models.Environment, error)

	// Reconciliation
	ReconcileStaleProvisioning(ctx context.Context, olderThan time.Duration) (int64, error)
}

type PostgresEnvironmentRepository struct {
	db *pgxpool.Pool
}

func NewPostgresEnvironmentRepository(db *pgxpool.Pool) *PostgresEnvironmentRepository {
	return &PostgresEnvironmentRepository{db: db}
}

// envColumns is the canonical ordered column list used in all SELECT/RETURNING clauses.
const envColumns = `id, user_email, name, image, status, container_id, creation_mode, runtime_target, cloud_status, cloud_region, cloud_instance_type, cloud_key_name, instance_id, public_ip, terraform_dir, cloud_error, cloud_provisioned_at, last_activity_at, created_at, updated_at`

func scanEnv(row interface {
	Scan(dest ...any) error
}, env *models.Environment) error {
	return row.Scan(
		&env.ID,
		&env.UserEmail,
		&env.Name,
		&env.Image,
		&env.Status,
		&env.ContainerID,
		&env.CreationMode,
		&env.RuntimeTarget,
		&env.CloudStatus,
		&env.CloudRegion,
		&env.CloudInstanceType,
		&env.CloudKeyName,
		&env.InstanceID,
		&env.PublicIP,
		&env.TerraformDir,
		&env.CloudError,
		&env.CloudProvisionedAt,
		&env.LastActivityAt,
		&env.CreatedAt,
		&env.UpdatedAt,
	)
}

func (r *PostgresEnvironmentRepository) Create(ctx context.Context, userEmail, name, image, status, containerID, creationMode string) (*models.Environment, error) {
	if r.db == nil {
		return nil, errors.New("database connection is nil")
	}

	if creationMode == "" {
		creationMode = "local"
	}

	query := `
		INSERT INTO environments (user_email, name, image, status, container_id, creation_mode)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING ` + envColumns

	var env models.Environment
	if err := scanEnv(r.db.QueryRow(ctx, query, userEmail, name, image, status, containerID, creationMode), &env); err != nil {
		return nil, err
	}
	return &env, nil
}

func (r *PostgresEnvironmentRepository) ListByUserEmail(ctx context.Context, userEmail string) ([]models.Environment, error) {
	if r.db == nil {
		return nil, errors.New("database connection is nil")
	}

	query := `
		SELECT ` + envColumns + `
		FROM environments
		WHERE user_email = $1
		ORDER BY created_at DESC`

	rows, err := r.db.Query(ctx, query, userEmail)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	environments := make([]models.Environment, 0)
	for rows.Next() {
		var env models.Environment
		if err := scanEnv(rows, &env); err != nil {
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

	query := `
		SELECT ` + envColumns + `
		FROM environments
		WHERE id = $1 AND user_email = $2`

	var env models.Environment
	err := scanEnv(r.db.QueryRow(ctx, query, id, userEmail), &env)
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

	query := `
		UPDATE environments
		SET status = $3, updated_at = NOW()
		WHERE id = $1 AND user_email = $2
		RETURNING ` + envColumns

	var env models.Environment
	err := scanEnv(r.db.QueryRow(ctx, query, id, userEmail, status), &env)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrEnvironmentNotFound
	}
	if err != nil {
		return nil, err
	}
	return &env, nil
}

func (r *PostgresEnvironmentRepository) UpdateProvisioning(ctx context.Context, id, userEmail, cloudStatus, cloudRegion, cloudInstanceType, cloudKeyName, instanceID, publicIP, terraformDir, cloudError string, cloudProvisionedAt *time.Time) (*models.Environment, error) {
	if r.db == nil {
		return nil, errors.New("database connection is nil")
	}

	query := `
		UPDATE environments
		SET
			cloud_status = $3,
			cloud_region = $4,
			cloud_instance_type = $5,
			cloud_key_name = $6,
			instance_id = $7,
			public_ip = $8,
			terraform_dir = $9,
			cloud_error = $10,
			cloud_provisioned_at = $11,
			updated_at = NOW()
		WHERE id = $1 AND user_email = $2
		RETURNING ` + envColumns

	var env models.Environment
	err := scanEnv(r.db.QueryRow(ctx, query, id, userEmail, cloudStatus, cloudRegion, cloudInstanceType, cloudKeyName, instanceID, publicIP, terraformDir, cloudError, cloudProvisionedAt), &env)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrEnvironmentNotFound
	}
	if err != nil {
		return nil, err
	}
	return &env, nil
}

func (r *PostgresEnvironmentRepository) UpdateRuntime(ctx context.Context, id, userEmail, runtimeTarget, containerID, status string) (*models.Environment, error) {
	if r.db == nil {
		return nil, errors.New("database connection is nil")
	}

	query := `
		UPDATE environments
		SET
			runtime_target = $3,
			container_id = $4,
			status = $5,
			updated_at = NOW()
		WHERE id = $1 AND user_email = $2
		RETURNING ` + envColumns

	var env models.Environment
	err := scanEnv(r.db.QueryRow(ctx, query, id, userEmail, runtimeTarget, containerID, status), &env)
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

	const query = `DELETE FROM environments WHERE id = $1 AND user_email = $2`
	result, err := r.db.Exec(ctx, query, id, userEmail)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrEnvironmentNotFound
	}
	return nil
}

// UpdateLastActivity refreshes the last_activity_at timestamp for an environment.
// This is an internal maintenance operation and does not require a userEmail guard.
func (r *PostgresEnvironmentRepository) UpdateLastActivity(ctx context.Context, id string) error {
	if r.db == nil {
		return errors.New("database connection is nil")
	}
	_, err := r.db.Exec(ctx, `UPDATE environments SET last_activity_at = NOW() WHERE id = $1`, id)
	return err
}

// ListRunningIdleSince returns all running environments whose last_activity_at is before
// the given cutoff time. Used by the lifecycle worker to find idle environments to stop.
func (r *PostgresEnvironmentRepository) ListRunningIdleSince(ctx context.Context, since time.Time) ([]models.Environment, error) {
	if r.db == nil {
		return nil, errors.New("database connection is nil")
	}

	query := `
		SELECT ` + envColumns + `
		FROM environments
		WHERE status = 'running'
		  AND last_activity_at < $1`

	rows, err := r.db.Query(ctx, query, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	environments := make([]models.Environment, 0)
	for rows.Next() {
		var env models.Environment
		if err := scanEnv(rows, &env); err != nil {
			return nil, err
		}
		environments = append(environments, env)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return environments, nil
}

// ReconcileStaleProvisioning marks environments stuck in a transitional cloud_status
// (provisioning or deprovisioning) for longer than olderThan with no active operation
// as provision_failed. Returns the number of environments updated.
func (r *PostgresEnvironmentRepository) ReconcileStaleProvisioning(ctx context.Context, olderThan time.Duration) (int64, error) {
	if r.db == nil {
		return 0, errors.New("database connection is nil")
	}

	cutoff := time.Now().Add(-olderThan)
	const query = `
		UPDATE environments
		SET
			cloud_status = 'provision_failed',
			cloud_error  = 'provisioning timed out: no active operation found during reconciliation',
			updated_at   = NOW()
		WHERE cloud_status IN ('provisioning', 'deprovisioning')
		  AND updated_at < $1
		  AND NOT EXISTS (
			SELECT 1
			FROM operations
			WHERE environment_id = environments.id::text
			  AND status IN ('queued', 'running')
		  )`

	result, err := r.db.Exec(ctx, query, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}
