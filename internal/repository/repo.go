package repository

import (
	"database/sql"

	_ "github.com/lib/pq"
)

type PostgresRepo struct {
	DB *sql.DB // Большая буква обязательна для экспорта
}

func NewPostgresRepo(connStr string) (*PostgresRepo, error) {
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, err
	}
	return &PostgresRepo{DB: db}, nil
}

// Методы для сервиса
func (r *PostgresRepo) GetAll() ([]string, error) {
	return []string{"Тестовый Пациент"}, nil
}
func (r *PostgresRepo) CreateAppointment(name string) error { return nil }
