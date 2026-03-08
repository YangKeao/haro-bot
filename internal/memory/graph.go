package memory

import "context"

type GraphStore interface {
	UpsertEntities(ctx context.Context, entities []GraphEntity) error
	UpsertRelations(ctx context.Context, relations []GraphRelation) error
	Query(ctx context.Context, query string) ([]GraphResult, error)
}

type GraphEntity struct {
	ID   string
	Name string
	Type string
}

type GraphRelation struct {
	Source string
	Rel    string
	Target string
}

type GraphResult struct {
	Source string
	Rel    string
	Target string
}

type NoopGraphStore struct{}

func NewNoopGraphStore() *NoopGraphStore {
	return &NoopGraphStore{}
}

func (NoopGraphStore) UpsertEntities(ctx context.Context, entities []GraphEntity) error {
	return nil
}

func (NoopGraphStore) UpsertRelations(ctx context.Context, relations []GraphRelation) error {
	return nil
}

func (NoopGraphStore) Query(ctx context.Context, query string) ([]GraphResult, error) {
	return nil, nil
}
