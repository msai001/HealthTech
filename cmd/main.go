package main

import (
	"context"
	"encoding/json"
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
	Scopes:       []string{"https://www.googleapis.com/auth/userinfo.email", "https://www.googleapis.com/auth/userinfo.profile"},
	Endpoint:     google.Endpoint,
}

const sharedStyles = `
	<style>
		* { box-sizing: border-box; margin: 0; padding: 0; }
		body { font-family: 'Inter', sans-serif; background: #f0f4f8; color: #333; }
		.container { display: flex; align-items: center; justify-content: center; min-height: 100vh; padding: 20px; }
		.card { background: white; padding: 40px; border-radius: 16px; box-shadow: 0 10px 25px rgba(0,0,0,0.05); width: 100%%; max-width: 450px; text-align: center; }
		.user-badge { background: #e2e8f0; padding: 8px 12px; border-radius: 20px; display: inline-block; font-size: 13px; font-weight: 600; color: #475569; margin-bottom: 20px; }
		h1 { color: #1a365d; margin-bottom: 10px; font-size: 28px; }
		p { color: #64748b; margin-bottom: 30px; }
		.btn { display: inline-block; background: #3b82f6; color: white; padding: 12px 24px; border-radius: 8px; text-decoration: none; font-weight: 600; border: none; cursor: pointer; width: 100%%; transition: 0.2s; }
		.btn:hover { background: #2563eb; }
		.form-group { text-align: left; margin-bottom: 20px; }
		label { display: block; font-size: 14px; font-weight: 600; margin-bottom: 6px; }
		input, select { width: 100%%; padding: 12px; border: 1px solid #e2e8f0; border-radius: 8px; font-size: 16px; }
	</style>
`

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		url := googleOAuthConfig.AuthCodeURL("state")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<html><head>%s</head><body>
			<div class="container"><div class="card">
				<h1>HealthTech</h1>
				<p>Войдите в систему для записи пациентов.</p>
				<a href="%s" class="btn">Войти через Google</a>
			</div></div>
		</body></html>`, sharedStyles, url)
	})

	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "Код не получен", http.StatusBadRequest)
			return
		}

		token, err := googleOAuthConfig.Exchange(context.Background(), code)
		if err != nil {
			http.Error(w, "Ошибка токена", http.StatusInternalServerError)
			return
		}

		// ШАГ 2: Получаем данные пользователя из Google
		client := googleOAuthConfig.Client(context.Background(), token)
		resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
		if err != nil {
			http.Error(w, "Ошибка получения данных", http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()

		var userInfo struct {
			Email string `json:"email"`
		}
		json.NewDecoder(resp.Body).Decode(&userInfo)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<html><head>%s</head><body>
			<div class="container"><div class="card">
				<div class="user-badge">👤 %s</div>
				<h1>Запись на прием</h1>
				<form action="/save" method="POST">
					<div class="form-group"><label>ФИО пациента</label><input type="text" name="name" required></div>
					<div class="form-group"><label>Дата</label><input type="date" name="date" required></div>
					<div class="form-group"><label>Специалист</label>
						<select name="doctor"><option>Терапевт</option><option>Хирург</option><option>Кардиолог</option></select>
					</div>
					<button type="submit" class="btn">Записать</button>
				</form>
			</div></div>
		</body></html>`, sharedStyles, userInfo.Email)
	})

	http.HandleFunc("/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		name := r.FormValue("name")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<html><head>%s</head><body>
			<div class="container"><div class="card">
				<h1 style="color:#27ae60;">✅ Успешно</h1>
				<p>Пациент <strong>%s</strong> добавлен в очередь.</p>
				<a href="/" class="btn" style="background:#64748b;">На главную</a>
			</div></div>
		</body></html>`, sharedStyles, name)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
