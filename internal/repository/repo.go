package repository

import (
	"database/sql"
	"health-app/internal/domain"

	_ "github.com/lib/pq"
)

type PostgresRepo struct {
	Db *sql.DB
}

func NewPostgresRepo(connStr string) (*PostgresRepo, error) {
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, err
	}
	return &PostgresRepo{Db: db}, nil
}

func (r *PostgresRepo) CreateAppointment(name string) error {
	query := `INSERT INTO appointments (patient_name, status) VALUES ($1, 'pending')`
	_, err := r.Db.Exec(query, name)
	return err
}

// Теперь принимает строку search
func (r *PostgresRepo) GetAll(search string) ([]domain.Appointment, error) {
	var rows *sql.Rows
	var err error

	if search == "" {
		rows, err = r.Db.Query("SELECT id, patient_name, status FROM appointments ORDER BY id DESC")
	} else {
		// Поиск по части имени (регистр не важен)
		query := "SELECT id, patient_name, status FROM appointments WHERE patient_name ILIKE $1 ORDER BY id DESC"
		rows, err = r.Db.Query(query, "%"+search+"%")
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var apps []domain.Appointment
	for rows.Next() {
		var a domain.Appointment
		if err := rows.Scan(&a.ID, &a.PatientName, &a.Status); err != nil {
			return nil, err
		}
		apps = append(apps, a)
	}
	return apps, nil
}

func (r *PostgresRepo) Update(id string, newName string) error {
	_, err := r.Db.Exec("UPDATE appointments SET patient_name = $1 WHERE id = $2", newName, id)
	return err
}

func (r *PostgresRepo) Delete(id string) error {
	_, err := r.Db.Exec("DELETE FROM appointments WHERE id = $1", id)
	return err
}
