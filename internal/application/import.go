package application

import (
	"context"
	"github.com/akkaraponph/evaris/internal/domain"
)

func (s Service) Import(ctx context.Context, store, record string) (domain.Run, error) {
	return s.Repo.Import(ctx, store, record)
}
