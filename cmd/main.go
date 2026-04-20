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
			if r.Method != http.MethodPost {
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}

			// Получаем данные из формы
			name := r.FormValue("name")
			date := r.FormValue("date")
			doctor := r.FormValue("doctor")

			// Пока мы не настроили базу на Render, просто выведем подтверждение
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, `
			<html><body style="text-align:center;padding:50px;font-family:Arial;">
				<h2 style="color: #27ae60;">Запись успешно создана!</h2>
				<p>Пациент: <b>%s</b></p>
				<p>Дата: %s</p>
				<p>Врач: %s</p>
				<br>
				<a href="/callback" style="color: #4285F4;">Вернуться к форме</a>
			</body></html>
		`, name, date, doctor)

			log.Printf("Новая запись: %s, %s, %s", name, date, doctor)
		})

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `
			<html>
			<head>
				<style>
					body { font-family: Arial; background: #f4f7f6; display: flex; justify-content: center; padding: 20px; }
					.card { background: white; padding: 30px; border-radius: 10px; box-shadow: 0 2px 10px rgba(0,0,0,0.1); width: 400px; }
					input, select, textarea { width: 100%%; margin-bottom: 15px; padding: 10px; border: 1px solid #ddd; border-radius: 5px; }
					button { width: 100%%; background: #27ae60; color: white; border: none; padding: 10px; border-radius: 5px; cursor: pointer; }
				</style>
			</head>
			<body>
				<div class="card">
					<h2>Запись пациента</h2>
					<form action="/save" method="POST">
						<label>Имя пациента</label>
						<input type="text" name="name" required>
						<label>Дата приема</label>
						<input type="date" name="date" required>
						<label>Врач</label>
						<select name="doctor">
							<option>Терапевт</option>
							<option>Хирург</option>
						</select>
						<button type="submit">Записать</button>
					</form>
				</div>
			</body>
			</html>
		`)
	})

	// 3. Запуск сервера
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Server starting on port %s...", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
