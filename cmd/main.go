package main

import (
	"fmt"
	"health-app/internal/repository"
	"health-app/internal/service"
	"log"
	"net/http"
	"os"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Конфигурация объявлена глобально, чтобы быть доступной в хендлерах
var googleOAuthConfig = &oauth2.Config{
	ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
	ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
	RedirectURL:  "https://healthtech-1.onrender.com/callback",
	Scopes:       []string{"https://www.googleapis.com/auth/userinfo.email"},
	Endpoint:     google.Endpoint,
}

func main() {
	// Подключение к базе данных
	connStr := "host=127.0.0.1 port=5432 user=postgres password=atyrau2026 dbname=postgres sslmode=disable"

	repo, err := repository.NewPostgresRepo(connStr)
	if err != nil {
		log.Printf("Предупреждение: не удалось подключиться к БД: %v", err)
		// На этапе тестов можно не выходить через log.Fatal, если БД на Render настроена иначе
	}
	_ = service.NewHealthService(repo) // инициализация сервиса

	// Хендлер главной страницы
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		url := googleOAuthConfig.AuthCodeURL("state")

		fmt.Fprintf(w, `
			<html>
				<head><title>HealthTech</title></head>
				<body style="font-family: Arial, sans-serif; text-align: center; padding-top: 50px;">
					<h1>HealthTech System</h1>
					<div style="margin-bottom: 30px;">
						<a href="%s" style="background: #4285F4; color: white; padding: 12px 24px; text-decoration: none; border-radius: 4px; font-weight: bold;">
							Войти через Google
						</a>
					</div>
					<hr>
					<p>Добро пожаловать в систему записи пациентов</p>
				</body>
			</html>
		`, url)
	})

	// Здесь должны быть ваши остальные хендлеры, например для /callback
	// http.HandleFunc("/callback", yourCallbackHandler)

	// Запуск сервера
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server starting on :%s...", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
