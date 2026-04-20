package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	_ "github.com/lib/pq"
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

var db *sql.DB

const sharedStyles = `
	<style>
		* { box-sizing: border-box; margin: 0; padding: 0; }
		body { font-family: 'Inter', sans-serif; background: #f0f4f8; color: #333; }
		.container { display: flex; align-items: center; justify-content: center; min-height: 100vh; padding: 20px; }
		.card { background: white; padding: 40px; border-radius: 16px; box-shadow: 0 10px 25px rgba(0,0,0,0.05); width: 100%%; max-width: 450px; text-align: center; }
		.user-badge { background: #e2e8f0; padding: 8px 12px; border-radius: 20px; display: inline-block; font-size: 12px; font-weight: 600; color: #475569; margin-bottom: 20px; }
		h1 { color: #1a365d; margin-bottom: 10px; font-size: 24px; }
		.btn { display: inline-block; background: #3b82f6; color: white; padding: 12px 24px; border-radius: 8px; text-decoration: none; font-weight: 600; border: none; cursor: pointer; width: 100%%; margin-top: 10px; }
		.form-group { text-align: left; margin-bottom: 15px; }
		label { display: block; font-size: 14px; font-weight: 600; margin-bottom: 5px; }
		input, select { width: 100%%; padding: 10px; border: 1px solid #e2e8f0; border-radius: 8px; }
	</style>
`

func initDB() {
	connStr := os.Getenv("DATABASE_URL")
	var err error

	// Пробуем подключиться
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal("Ошибка конфигурации БД:", err)
	}

	// Проверяем жива ли база
	err = db.Ping()
	if err != nil {
		log.Printf("База не отвечает, пробуем SSL режим...")
		db, _ = sql.Open("postgres", connStr+"?sslmode=require")
	}

	// Создаем таблицу если её нет
	query := `
	CREATE TABLE IF NOT EXISTS appointments (
		id SERIAL PRIMARY KEY,
		patient_name TEXT NOT NULL,
		appointment_date TEXT NOT NULL,
		doctor_name TEXT NOT NULL,
		user_email TEXT NOT NULL
	);`
	_, err = db.Exec(query)
	if err != nil {
		log.Printf("Ошибка при создании таблицы: %v", err)
	}
}

func main() {
	initDB()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		url := googleOAuthConfig.AuthCodeURL("state")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<html><head>%s</head><body>
			<div class="container"><div class="card">
				<h1>HealthTech</h1>
				<p>Система записи пациентов</p>
				<a href="%s" class="btn">Войти через Google</a>
			</div></div>
		</body></html>`, sharedStyles, url)
	})

	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		token, err := googleOAuthConfig.Exchange(context.Background(), code)
		if err != nil {
			http.Error(w, "Ошибка авторизации", 500)
			return
		}

		client := googleOAuthConfig.Client(context.Background(), token)
		resp, _ := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
		var userInfo struct{ Email string }
		json.NewDecoder(resp.Body).Decode(&userInfo)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<html><head>%s</head><body>
			<div class="container"><div class="card">
				<div class="user-badge">👤 %s</div>
				<h1>Новая запись</h1>
				<form action="/save" method="POST">
					<input type="hidden" name="user_email" value="%s">
					<div class="form-group"><label>ФИО пациента</label><input type="text" name="patient_name" required></div>
					<div class="form-group"><label>Дата</label><input type="date" name="date" required></div>
					<div class="form-group"><label>Врач</label>
						<select name="doctor"><option>Терапевт</option><option>Хирург</option><option>Кардиолог</option></select>
					</div>
					<button type="submit" class="btn">Сохранить в базу</button>
				</form>
			</div></div>
		</body></html>`, sharedStyles, userInfo.Email, userInfo.Email)
	})

	http.HandleFunc("/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Redirect(w, r, "/", 302)
			return
		}

		pName := r.FormValue("patient_name")
		pDate := r.FormValue("date")
		pDoc := r.FormValue("doctor")
		uEmail := r.FormValue("user_email")

		// Если email пустой, ставим заглушку
		if uEmail == "" {
			uEmail = "anonymous@healthtech.com"
		}

		_, err := db.Exec("INSERT INTO appointments (patient_name, appointment_date, doctor_name, user_email) VALUES ($1, $2, $3, $4)",
			pName, pDate, pDoc, uEmail)

		if err != nil {
			log.Printf("КРИТИЧЕСКАЯ ОШИБКА БД: %v", err)
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			fmt.Fprintf(w, "ОШИБКА СОХРАНЕНИЯ В БАЗУ:\n\n%v\n\nПроверь логи Render для деталей.", err)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<html><head>%s</head><body>
			<div class="container"><div class="card">
				<h1 style="color:#27ae60;">✅ Успешно!</h1>
				<p>Пациент <b>%s</b> записан в базу.</p>
				<a href="/" class="btn" style="background:#64748b;">На главную</a>
			</div></div>
		</body></html>`, sharedStyles, pName)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
