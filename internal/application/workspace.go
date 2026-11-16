package application

import "context"

// Init creates an editable experiment workspace without changing existing inputs.
func (s Service) Init(ctx context.Context, root string) (string, error) {
	return s.Repo.Init(ctx, root)
}
