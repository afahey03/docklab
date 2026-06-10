package repositories

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/afahey03/docklab/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrEnvironmentNotFound = errors.New("environment not found")

type EnvironmentRepository interface {
	Create(ctx context.Context, userEmail, name, image, status, containerID, creationMode, repoURL, templateID string) (*models.Environment, error)
	ListByUserEmail(ctx context.Context, userEmail string) ([]models.Environment, error)
	ListSharedWithUser(ctx context.Context, userEmail string) ([]models.Environment, error)
	GetByIDForUser(ctx context.Context, id, userEmail string) (*models.Environment, error)
	GetByID(ctx context.Context, id string) (*models.Environment, error)
	CountByUserEmail(ctx context.Context, userEmail string) (int, error)
	UpdateStatus(ctx context.Context, id, userEmail, status string) (*models.Environment, error)
	UpdateProvisioning(ctx context.Context, id, userEmail, cloudStatus, cloudRegion, cloudInstanceType, cloudKeyName, instanceID, publicIP, terraformDir, cloudError string, cloudProvisionedAt *time.Time) (*models.Environment, error)
	UpdateRuntime(ctx context.Context, id, userEmail, runtimeTarget, containerID, status string) (*models.Environment, error)
	Delete(ctx context.Context, id, userEmail string) error

	// Activity tracking
	UpdateLastActivity(ctx context.Context, id string) error
	ListRunningIdleSince(ctx context.Context, since time.Time) ([]models.Environment, error)

	// Reconciliation
	ReconcileStaleProvisioning(ctx context.Context, olderThan time.Duration) (int64, error)
	ReconcileMissingCloudInstances(ctx context.Context, instanceIDs []string) (int64, error)

	// Cloud lifecycle
	ListIdleProvisionedCloudSince(ctx context.Context, since time.Time) ([]models.Environment, error)
	ListIdleStoppedCloudSince(ctx context.Context, since time.Time) ([]models.Environment, error)
	ListWithCloudInstanceID(ctx context.Context) ([]models.Environment, error)
}

type PostgresEnvironmentRepository struct {
	db *pgxpool.Pool
}

func NewPostgresEnvironmentRepository(db *pgxpool.Pool) *PostgresEnvironmentRepository {
	return &PostgresEnvironmentRepository{db: db}
}

// envColumns is the canonical ordered column list used in all SELECT/RETURNING clauses.
const envColumns = `id, user_email, name, image, status, container_id, creation_mode, repo_url, template_id, runtime_target, cloud_status, cloud_region, cloud_instance_type, cloud_key_name, instance_id, public_ip, terraform_dir, cloud_error, cloud_provisioned_at, last_activity_at, created_at, updated_at`

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
		&env.RepoURL,
		&env.TemplateID,
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

func (r *PostgresEnvironmentRepository) Create(ctx context.Context, userEmail, name, image, status, containerID, creationMode, repoURL, templateID string) (*models.Environment, error) {
	if r.db == nil {
		return nil, errors.New("database connection is nil")
	}

	if creationMode == "" {
		creationMode = "local"
	}

	query := `
		INSERT INTO environments (user_email, name, image, status, container_id, creation_mode, repo_url, template_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING ` + envColumns

	var env models.Environment
	if err := scanEnv(r.db.QueryRow(ctx, query, userEmail, name, image, status, containerID, creationMode, repoURL, templateID), &env); err != nil {
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

func (r *PostgresEnvironmentRepository) ListSharedWithUser(ctx context.Context, userEmail string) ([]models.Environment, error) {
	if r.db == nil {
		return nil, errors.New("database connection is nil")
	}

	query := `
		SELECT ` + qualifiedEnvColumns("e") + `
		FROM environments e
		INNER JOIN environment_shares s ON s.environment_id = e.id
		WHERE s.shared_with_email = $1
		ORDER BY e.created_at DESC`

	return r.queryEnvironments(ctx, query, userEmail)
}

func (r *PostgresEnvironmentRepository) CountByUserEmail(ctx context.Context, userEmail string) (int, error) {
	if r.db == nil {
		return 0, errors.New("database connection is nil")
	}

	var count int
	if err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM environments WHERE user_email = $1`, userEmail).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (r *PostgresEnvironmentRepository) GetByID(ctx context.Context, id string) (*models.Environment, error) {
	if r.db == nil {
		return nil, errors.New("database connection is nil")
	}

	query := `
		SELECT ` + envColumns + `
		FROM environments
		WHERE id = $1`

	var env models.Environment
	err := scanEnv(r.db.QueryRow(ctx, query, id), &env)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrEnvironmentNotFound
	}
	if err != nil {
		return nil, err
	}
	return &env, nil
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

func (r *PostgresEnvironmentRepository) ListIdleProvisionedCloudSince(ctx context.Context, since time.Time) ([]models.Environment, error) {
	if r.db == nil {
		return nil, errors.New("database connection is nil")
	}

	query := `
		SELECT ` + envColumns + `
		FROM environments
		WHERE cloud_status = 'provisioned'
		  AND runtime_target = 'remote'
		  AND instance_id <> ''
		  AND last_activity_at < $1`

	return r.queryEnvironments(ctx, query, since)
}

func (r *PostgresEnvironmentRepository) ListIdleStoppedCloudSince(ctx context.Context, since time.Time) ([]models.Environment, error) {
	if r.db == nil {
		return nil, errors.New("database connection is nil")
	}

	query := `
		SELECT ` + envColumns + `
		FROM environments
		WHERE cloud_status = 'cloud_stopped'
		  AND instance_id <> ''
		  AND last_activity_at < $1`

	return r.queryEnvironments(ctx, query, since)
}

func (r *PostgresEnvironmentRepository) ListWithCloudInstanceID(ctx context.Context) ([]models.Environment, error) {
	if r.db == nil {
		return nil, errors.New("database connection is nil")
	}

	query := `
		SELECT ` + envColumns + `
		FROM environments
		WHERE instance_id <> ''
		  AND cloud_status IN ('provisioned', 'cloud_stopped')`

	return r.queryEnvironments(ctx, query)
}

func (r *PostgresEnvironmentRepository) ReconcileMissingCloudInstances(ctx context.Context, instanceIDs []string) (int64, error) {
	if r.db == nil {
		return 0, errors.New("database connection is nil")
	}
	if len(instanceIDs) == 0 {
		return 0, nil
	}

	const query = `
		UPDATE environments
		SET
			cloud_status = 'not_provisioned',
			instance_id = '',
			public_ip = '',
			terraform_dir = '',
			cloud_error = $2,
			cloud_provisioned_at = NULL,
			runtime_target = 'local',
			status = 'stopped',
			container_id = 'pending-reconciled-' || id::text,
			updated_at = NOW()
		WHERE instance_id = ANY($1)
		  AND cloud_status IN ('provisioned', 'cloud_stopped')`

	result, err := r.db.Exec(ctx, query, instanceIDs, orphanReconcileMessage())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func orphanReconcileMessage() string {
	return "EC2 instance no longer exists in AWS; cloud resources cleared during reconciliation"
}

// qualifiedEnvColumns prefixes every environment column with a table alias for joins.
func qualifiedEnvColumns(alias string) string {
	parts := strings.Split(envColumns, ", ")
	for i, part := range parts {
		parts[i] = alias + "." + part
	}
	return strings.Join(parts, ", ")
}

func (r *PostgresEnvironmentRepository) queryEnvironments(ctx context.Context, query string, args ...any) ([]models.Environment, error) {
	rows, err := r.db.Query(ctx, query, args...)
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
