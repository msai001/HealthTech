package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Глобальная переменная для конфига
var googleOAuthConfig = &oauth2.Config{
	ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
	ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
	RedirectURL:  "https://healthtech-1.onrender.com/callback",
	Scopes:       []string{"https://www.googleapis.com/auth/userinfo.email"},
	Endpoint:     google.Endpoint,
}

func main() {
	// 1. Главная страница
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		url := googleOAuthConfig.AuthCodeURL("state")
		fmt.Fprintf(w, `<html><body style="text-align:center;padding:50px;font-family:Arial;">
			<h1>HealthTech</h1>
			<a href="%s" style="background:#4285F4;color:white;padding:15px;text-decoration:none;border-radius:5px;font-weight:bold;">Войти через Google</a>
		</body></html>`, url)
	})

	// 2. Обработчик Callback
	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "No code in request", http.StatusBadRequest)
			return
		}

		// Обмениваем код на токен
		token, err := googleOAuthConfig.Exchange(context.Background(), code)
		if err != nil {
			log.Printf("Token exchange error: %v", err)
			http.Error(w, "Failed to exchange token", http.StatusInternalServerError)
			return
		}

		// Используем токен, чтобы Go не ругался (UnusedVar)
		log.Printf("Token received! Type: %s", token.TokenType)

		fmt.Fprintf(w, "<h1>Успешный вход!</h1><p>Вы успешно авторизованы в HealthTech.</p>")
	})

	// 3. Запуск сервера (обязательно через переменную PORT для Render)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server is starting on port %s...", port)
	err := http.ListenAndServe(":"+port, nil)
	if err != nil {
		log.Fatal(err)
	}
}
