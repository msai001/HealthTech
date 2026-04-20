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

const sharedStyles = `
	<style>
		* { box-sizing: border-box; margin: 0; padding: 0; }
		body { font-family: 'Inter', -apple-system, sans-serif; background: #f0f4f8; color: #333; }
		.container { display: flex; align-items: center; justify-content: center; min-height: 100vh; padding: 20px; }
		.card { background: white; padding: 40px; border-radius: 16px; box-shadow: 0 10px 25px rgba(0,0,0,0.05); width: 100%%; max-width: 450px; text-align: center; }
		h1 { color: #1a365d; margin-bottom: 10px; font-size: 28px; }
		p { color: #64748b; margin-bottom: 30px; }
		.btn-google { display: inline-flex; align-items: center; background: #fff; color: #1f2937; border: 1px solid #d1d5db; padding: 12px 24px; border-radius: 8px; text-decoration: none; font-weight: 600; }
		.form-group { text-align: left; margin-bottom: 20px; }
		label { display: block; font-size: 14px; font-weight: 600; margin-bottom: 6px; }
		input, select { width: 100%%; padding: 12px; border: 1px solid #e2e8f0; border-radius: 8px; font-size: 16px; }
		.btn-submit { width: 100%%; background: #3b82f6; color: white; border: none; padding: 14px; border-radius: 8px; font-size: 16px; font-weight: 600; cursor: pointer; }
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
					<p>Медицинская система записи. Авторизуйтесь для продолжения.</p>
					<a href="%s" class="btn-google">Войти через Google</a>
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
		_, err := googleOAuthConfig.Exchange(context.Background(), code)
		if err != nil {
			http.Error(w, "Ошибка авторизации", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<html><head>%s</head><body>
			<div class="container">
				<div class="card">
					<h1>Запись пациента</h1>
					<form action="/save" method="POST">
						<div class="form-group">
							<label>ФИО</label>
							<input type="text" name="name" required>
						</div>
						<div class="form-group">
							<label>Дата</label>
							<input type="date" name="date" required>
						</div>
						<div class="form-group">
							<label>Врач</label>
							<select name="doctor">
								<option>Терапевт</option>
								<option>Хирург</option>
							</select>
						</div>
						<button type="submit" class="btn-submit">Записать</button>
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
					<h1>✅ Готово</h1>
					<p>Пациент <strong>%s</strong> записан.</p>
					<a href="/" class="btn-submit" style="text-decoration:none; display:block;">На главную</a>
				</div>
			</div>
		</body></html>`, sharedStyles, name)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
