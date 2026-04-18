package service

import (
	"health-app/internal/domain"
	"health-app/internal/repository"
)

type HealthService struct {
	repo *repository.PostgresRepo
}

func NewHealthService(repo *repository.PostgresRepo) *HealthService {
	return &HealthService{repo: repo}
}

// Теперь прокидывает search в репозиторий
func (s *HealthService) GetList(search string) ([]domain.Appointment, error) {
	return s.repo.GetAll(search)
}

func (s *HealthService) Create(name string) error {
	return s.repo.CreateAppointment(name)
}

func (s *HealthService) Update(id string, newName string) error {
	return s.repo.Update(id, newName)
}

func (s *HealthService) Delete(id string) error {
	return s.repo.Delete(id)
}
