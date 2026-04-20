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

var googleOAuthConfig = &oauth2.Config{
	ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
	ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
	RedirectURL:  "https://healthtech-1.onrender.com/callback",
	Scopes:       []string{"https://www.googleapis.com/auth/userinfo.email"},
	Endpoint:     google.Endpoint,
}

func main() {
	// Главная страница с кнопкой
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		url := googleOAuthConfig.AuthCodeURL("state")
		fmt.Fprintf(w, `<html><body style="text-align:center;padding:50px;font-family:Arial;">
			<h1>HealthTech</h1>
			<a href="%s" style="background:#4285F4;color:white;padding:15px;text-decoration:none;border-radius:5px;">Войти через Google</a>
		</body></html>`, url)
	})

	// Обработчик после нажатия "Продолжить"
	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")

		// Используем переменную token, чтобы Go не ругался
		token, err := googleOAuthConfig.Exchange(context.Background(), code)
		if err != nil {
			http.Error(w, "Ошибка авторизации", http.StatusInternalServerError)
			return
		}

		// Выводим сообщение, используя токен (теперь он "использован")
		log.Printf("Авторизация успешна. Токен получен.")
		fmt.Fprintf(w, "<h1>Успех!</h1><p>Вы вошли в систему. Токен: %s</p>", token.AccessToken[:10]+"...")
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
