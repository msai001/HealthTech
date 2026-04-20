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

// В стилях ВСЕ проценты должны быть удвоены: %%
const sharedStyles = `
	<style>
		* { box-sizing: border-box; margin: 0; padding: 0; }
		body { font-family: 'Inter', sans-serif; background: #f0f4f8; color: #333; }
		.container { display: flex; align-items: center; justify-content: center; min-height: 100vh; padding: 20px; }
		.card { background: white; padding: 40px; border-radius: 16px; box-shadow: 0 10px 25px rgba(0,0,0,0.05); width: 100%%; max-width: 450px; text-align: center; }
		h1 { color: #1a365d; margin-bottom: 10px; font-size: 28px; }
		p { color: #64748b; margin-bottom: 30px; }
		.btn { display: inline-block; background: #3b82f6; color: white; padding: 12px 24px; border-radius: 8px; text-decoration: none; font-weight: 600; border: none; cursor: pointer; width: 100%%; }
		.form-group { text-align: left; margin-bottom: 20px; }
		label { display: block; font-size: 14px; font-weight: 600; margin-bottom: 6px; }
		input, select { width: 100%%; padding: 12px; border: 1px solid #e2e8f0; border-radius: 8px; font-size: 16px; }
	</style>
`

func main() {
	// 1. ГЛАВНАЯ
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		url := googleOAuthConfig.AuthCodeURL("state")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<html><head>%s</head><body>
			<div class="container">
				<div class="card">
					<h1>HealthTech</h1>
					<p>Медицинская система. Пожалуйста, войдите.</p>
					<a href="%s" class="btn">Войти через Google</a>
				</div>
			</div>
		</body></html>`, sharedStyles, url)
	})

	// 2. CALLBACK
	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "Код не получен", http.StatusBadRequest)
			return
		}
		// Обмениваем код на токен
		_, err := googleOAuthConfig.Exchange(context.Background(), code)
		if err != nil {
			log.Printf("Auth error: %v", err)
			http.Error(w, "Ошибка авторизации", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<html><head>%s</head><body>
			<div class="container">
				<div class="card">
					<h1>Запись пациента</h1>
					<form action="/save" method="POST">
						<div class="form-group"><label>ФИО</label><input type="text" name="name" required></div>
						<div class="form-group"><label>Дата</label><input type="date" name="date" required></div>
						<div class="form-group"><label>Врач</label>
							<select name="doctor"><option>Терапевт</option><option>Хирург</option></select>
						</div>
						<button type="submit" class="btn">Записать</button>
					</form>
				</div>
			</div>
		</body></html>`, sharedStyles)
	})

	// 3. СОХРАНЕНИЕ
	http.HandleFunc("/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		name := r.FormValue("name")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<html><head>%s</head><body>
			<div class="container">
				<div class="card">
					<h1 style="color:#27ae60;">✅ Готово</h1>
					<p>Пациент <strong>%s</strong> успешно записан.</p>
					<a href="/" class="btn" style="background:#64748b;">На главную</a>
				</div>
			</div>
		</body></html>`, sharedStyles, name)
	})

	// Запуск
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Server started on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
