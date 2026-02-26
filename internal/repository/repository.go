package repository

import (
	"context"
	"github.com/sakif/coding-playground/internal/model"
)

type ListOptions struct {
	Limit int
	Offset int
}

type SnippetRepository interface {
	Create(ctx context.Context, snippet *model.Snippet) error
	GetByID(ctx context.Context, id string) (*model.Snippet, error)
	List(ctx context.Context, opts ListOptions) ([]model.Snippet, error)
	Update(ctx context.Context, snippet *model.Snippet) error
	Delete(ctx context.Context, id string) error
}