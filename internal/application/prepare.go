package application

import (
	"context"
	"github.com/akkaraponph/evaris/internal/domain"
)

func (s Service) Prepare(ctx context.Context, config, tasksRoot string) (domain.Preparation, error) {
	p, e := s.Repo.Load(ctx, config)
	if e != nil {
		return domain.Preparation{}, e
	}
	return s.Repo.Prepare(ctx, p, tasksRoot)
}
