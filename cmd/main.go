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
	// 1. Главная страница
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		url := googleOAuthConfig.AuthCodeURL("state")
		fmt.Fprintf(w, `<html><body style="text-align:center;padding:50px;font-family:Arial;">
			<h1>HealthTech System</h1>
			<a href="%s" style="background:#4285F4;color:white;padding:15px;text-decoration:none;border-radius:5px;font-weight:bold;">Войти через Google</a>
		</body></html>`, url)
	})

	// 2. Обработчик Callback (выдает форму после входа)
	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "Код не получен", http.StatusBadRequest)
			return
		}

		token, err := googleOAuthConfig.Exchange(context.Background(), code)
		if err != nil {
			log.Printf("Token exchange error: %v", err)
			http.Error(w, "Ошибка авторизации", http.StatusInternalServerError)
			return
		}

		log.Printf("Авторизация успешна для токена: %s", token.TokenType)
		// 3. Обработчик сохранения (сработает при нажатии "Записать")
		http.HandleFunc("/save", func(w http.ResponseWriter, r *http.Request) {
			// ... весь внутренний код до закрывающейся })
		})

		// ... здесь заканчивается блок http.HandleFunc("/callback", ...)
	}) // <--- Это закрывающая скобка CALLBACK

	// ВСТАВЛЯЙ СЮДА:
	http.HandleFunc("/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		name := r.FormValue("name")
		date := r.FormValue("date")
		doctor := r.FormValue("doctor")

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `
			<html><body style="text-align:center;padding:50px;font-family:Arial;">
				<h2 style="color: #27ae60;">✅ Запись создана!</h2>
				<p>Пациент: <b>%s</b></p>
				<p>Дата: %s</p>
				<p>Врач: %s</p>
				<br>
				<a href="/" style="display:inline-block; background:#4285F4; color:white; padding:10px 20px; text-decoration:none; border-radius:5px;">На главную</a>
			</body></html>
		`, name, date, doctor)
	})

	// Дальше идет твой старый код запуска сервера:
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(":"+port, nil))
} // Конец функции main
