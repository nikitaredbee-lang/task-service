package service

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"github.com/nikitaredbee-lang/task-service/internal/domain"
)

// dummyHash нужен чтобы логин с несуществующим email занимал примерно столько же
// времени, сколько с существующим, и по времени ответа нельзя было перебирать почты.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("not-a-real-password"), bcrypt.DefaultCost)

type AuthService struct {
	users  UserRepository
	tokens TokenIssuer
}

func NewAuthService(users UserRepository, tokens TokenIssuer) *AuthService {
	return &AuthService{users: users, tokens: tokens}
}

type RegisterInput struct {
	Email    string
	Password string
	Name     string
}

func (s *AuthService) Register(ctx context.Context, in RegisterInput) (*domain.User, error) {
	email := normalizeEmail(in.Email)
	if addr, err := mail.ParseAddress(email); err != nil || addr.Address != email || utf8.RuneCountInString(email) > 255 {
		return nil, domain.Invalid("email is invalid")
	}
	// bcrypt принимает не больше 72 байт, в том числе для паролей с Unicode.
	if len(in.Password) < 8 || len(in.Password) > 72 {
		return nil, domain.Invalid("password must be from 8 to 72 bytes")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || utf8.RuneCountInString(name) > 100 {
		return nil, domain.Invalid("name must be from 1 to 100 characters")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	user := &domain.User{
		Email:        email,
		Name:         name,
		PasswordHash: string(hash),
		CreatedAt:    now(),
	}
	if err := s.users.Create(ctx, user); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return nil, domain.Conflict("email is already registered")
		}
		return nil, err
	}
	return user, nil
}

func (s *AuthService) Login(ctx context.Context, email, password string) (string, error) {
	user, err := s.users.GetByEmail(ctx, normalizeEmail(email))
	if errors.Is(err, domain.ErrNotFound) {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return "", domain.Unauthorized("invalid email or password")
	}
	if err != nil {
		return "", err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return "", domain.Unauthorized("invalid email or password")
	}
	return s.tokens.Issue(user.ID)
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
