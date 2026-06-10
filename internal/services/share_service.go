package services

import (
	"context"
	"errors"
	"strings"

	"github.com/afahey03/docklab/internal/models"
	"github.com/afahey03/docklab/internal/repositories"
)

var ErrCannotShareWithSelf = errors.New("cannot share an environment with yourself")
var ErrShareUserNotFound = errors.New("no DockLab user with that email exists")

// ShareService manages multi-user collaboration: owners grant other users access to
// view an environment and join its (shared) terminal session.
type ShareService struct {
	environmentRepo repositories.EnvironmentRepository
	shareRepo       repositories.ShareRepository
	userRepo        repositories.UserRepository
}

func NewShareService(
	environmentRepo repositories.EnvironmentRepository,
	shareRepo repositories.ShareRepository,
	userRepo repositories.UserRepository,
) *ShareService {
	return &ShareService{
		environmentRepo: environmentRepo,
		shareRepo:       shareRepo,
		userRepo:        userRepo,
	}
}

func (s *ShareService) ShareEnvironment(ctx context.Context, environmentID, ownerEmail, sharedWithEmail string) (*models.EnvironmentShare, error) {
	sharedWithEmail = strings.ToLower(strings.TrimSpace(sharedWithEmail))
	if sharedWithEmail == "" {
		return nil, ErrShareUserNotFound
	}
	if strings.EqualFold(sharedWithEmail, ownerEmail) {
		return nil, ErrCannotShareWithSelf
	}

	// Ownership check.
	if _, err := s.environmentRepo.GetByIDForUser(ctx, environmentID, ownerEmail); err != nil {
		return nil, err
	}

	// The target must be a real user so shares never dangle.
	if _, err := s.userRepo.GetByEmail(ctx, sharedWithEmail); err != nil {
		if errors.Is(err, repositories.ErrUserNotFound) {
			return nil, ErrShareUserNotFound
		}
		return nil, err
	}

	return s.shareRepo.Create(ctx, environmentID, ownerEmail, sharedWithEmail)
}

func (s *ShareService) ListShares(ctx context.Context, environmentID, ownerEmail string) ([]models.EnvironmentShare, error) {
	if _, err := s.environmentRepo.GetByIDForUser(ctx, environmentID, ownerEmail); err != nil {
		return nil, err
	}
	return s.shareRepo.ListForEnvironment(ctx, environmentID, ownerEmail)
}

func (s *ShareService) Unshare(ctx context.Context, environmentID, ownerEmail, sharedWithEmail string) error {
	if _, err := s.environmentRepo.GetByIDForUser(ctx, environmentID, ownerEmail); err != nil {
		return err
	}
	return s.shareRepo.Delete(ctx, environmentID, ownerEmail, strings.ToLower(strings.TrimSpace(sharedWithEmail)))
}

func (s *ShareService) ListSharedWithUser(ctx context.Context, userEmail string) ([]models.Environment, error) {
	return s.environmentRepo.ListSharedWithUser(ctx, userEmail)
}

// GetAccessibleEnvironment returns the environment when the user owns it or it has
// been shared with them. The second return value reports ownership.
func (s *ShareService) GetAccessibleEnvironment(ctx context.Context, environmentID, userEmail string) (*models.Environment, bool, error) {
	env, err := s.environmentRepo.GetByIDForUser(ctx, environmentID, userEmail)
	if err == nil {
		return env, true, nil
	}
	if !errors.Is(err, repositories.ErrEnvironmentNotFound) {
		return nil, false, err
	}

	shared, shareErr := s.shareRepo.IsSharedWith(ctx, environmentID, userEmail)
	if shareErr != nil {
		return nil, false, shareErr
	}
	if !shared {
		return nil, false, repositories.ErrEnvironmentNotFound
	}

	env, err = s.environmentRepo.GetByID(ctx, environmentID)
	if err != nil {
		return nil, false, err
	}
	return env, false, nil
}
