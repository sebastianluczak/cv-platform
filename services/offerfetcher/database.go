// database connection to postgresql
package main

import (
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"
)

type Database interface {
	Connect() (*sql.DB, error)
}

type PostgresDatabase struct {
	ConnStr string
}

func (p *PostgresDatabase) Connect() (*sql.DB, error) {
	db, err := sql.Open("postgres", p.ConnStr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}
	if err := p.CreateInitialSchemaForOffer(db); err != nil {
		return nil, fmt.Errorf("failed to create initial schema for offers: %w", err)
	}
	return db, nil
}

func (p *PostgresDatabase) CreateInitialSchemaForOffer(db *sql.DB) error {
	query := `
	CREATE TABLE IF NOT EXISTS offers (
		id SERIAL PRIMARY KEY,
		title TEXT NOT NULL,
		link TEXT NOT NULL
	);
	`
	_, err := db.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to create initial schema for offers: %w", err)
	}
	return nil
}

type OfferRecord struct {
	ID    int
	Title string
	Link  string
}

func (p *PostgresDatabase) InsertOffer(db *sql.DB, offer OfferRecord) error {
	query := "INSERT INTO offers (title, link) VALUES ($1, $2)"
	_, err := db.Exec(query, offer.Title, offer.Link)
	if err != nil {
		return fmt.Errorf("failed to insert offer: %w", err)
	}
	return nil
}

func (p *PostgresDatabase) GetOffers(db *sql.DB) ([]OfferRecord, error) {
	query := "SELECT id, title, link FROM offers"
	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query offers: %w", err)
	}
	defer rows.Close()

	var offers []OfferRecord
	for rows.Next() {
		var offer OfferRecord
		if err := rows.Scan(&offer.ID, &offer.Title, &offer.Link); err != nil {
			return nil, fmt.Errorf("failed to scan offer: %w", err)
		}
		offers = append(offers, offer)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return offers, nil
}
