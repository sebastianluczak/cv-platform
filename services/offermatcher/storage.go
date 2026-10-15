package main

import (
	"context"
	"fmt"
)

type Storage interface {
	Save(ctx context.Context, u User) error
}

type PostgresStorage struct{}

func (p *PostgresStorage) Save(
	ctx context.Context,
	u User,
) error {
	// do the logic here
	fmt.Println("Saving a user!")
	return nil
}
