package service

import "health-app/internal/repository"

type HealthService struct {
	repo *repository.PostgresRepo
}

func NewHealthService(r *repository.PostgresRepo) *HealthService {
	return &HealthService{repo: r}
}

func (s *HealthService) GetList(search string) ([]string, error) {
	return s.repo.GetAll()
}
