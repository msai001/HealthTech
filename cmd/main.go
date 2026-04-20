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
		body { font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; background: #f4f7f6; color: #333; }
		.container { max-width: 900px; margin: 40px auto; padding: 20px; }
		.card { background: white; padding: 30px; border-radius: 12px; box-shadow: 0 4px 6px rgba(0,0,0,0.1); margin-bottom: 30px; }
		h1 { color: #2c3e50; margin-bottom: 20px; text-align: center; }
		.user-info { text-align: right; margin-bottom: 10px; font-weight: bold; color: #7f8c8d; }
		.btn { display: block; width: 100%; padding: 12px; background: #3498db; color: white; text-align: center; border-radius: 6px; text-decoration: none; border: none; font-size: 16px; cursor: pointer; }
		.btn:hover { background: #2980b9; }
		.form-group { margin-bottom: 15px; }
		label { display: block; margin-bottom: 5px; font-weight: 600; }
		input, select { width: 100%; padding: 10px; border: 1px solid #ddd; border-radius: 4px; }
		table { width: 100%; border-collapse: collapse; margin-top: 20px; }
		th, td { padding: 12px; border: 1px solid #eee; text-align: left; }
		th { background: #f8f9fa; }
		tr:nth-child(even) { background: #fcfcfc; }
		.empty-msg { text-align: center; color: #95a5a6; padding: 20px; }
	</style>
`

func initDB() {
	connStr := os.Getenv("DATABASE_URL")
	var err error
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal(err)
	}
	err = db.Ping()
	if err != nil {
		// Попытка с SSL для Render
		db, _ = sql.Open("postgres", connStr+"?sslmode=require")
	}
}

func main() {
	initDB()

	// Главная страница
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		url := googleOAuthConfig.AuthCodeURL("state")
		fmt.Fprintf(w, `<html><head>%s</head><body>
			<div class="container"><div class="card">
				<h1>HealthTech System</h1>
				<a href="%s" class="btn">Войти через Google</a>
			</div></div>
		</body></html>`, sharedStyles, url)
	})

	// Callback после логина
	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		token, err := googleOAuthConfig.Exchange(context.Background(), code)
		if err != nil {
			http.Redirect(w, r, "/", 302)
			return
		}

		client := googleOAuthConfig.Client(context.Background(), token)
		resp, _ := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
		var userInfo struct{ Email string }
		json.NewDecoder(resp.Body).Decode(&userInfo)

		// Загружаем записи из базы
		rows, err := db.Query("SELECT patient_name, appointment_date, doctor_name FROM appointments ORDER BY id DESC")
		var tableBody string
		if err != nil {
			tableBody = "<tr><td colspan='3' class='empty-msg'>Ошибка загрузки данных</td></tr>"
		} else {
			count := 0
			for rows.Next() {
				var pName, pDate, pDoc string
				rows.Scan(&pName, &pDate, &pDoc)
				tableBody += fmt.Sprintf("<tr><td>%s</td><td>%s</td><td>%s</td></tr>", pName, pDate, pDoc)
				count++
			}
			if count == 0 {
				tableBody = "<tr><td colspan='3' class='empty-msg'>Записей пока нет</td></tr>"
			}
			rows.Close()
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<html><head>%s</head><body>
			<div class="container">
				<div class="user-info">Вы вошли как: %s</div>
				<div class="card">
					<h1>Новая запись</h1>
					<form action="/save" method="POST">
						<input type="hidden" name="user_email" value="%s">
						<div class="form-group">
							<label>ФИО Пациента</label>
							<input type="text" name="patient_name" placeholder="Иван Иванов" required>
						</div>
						<div class="form-group">
							<label>Дата приема</label>
							<input type="date" name="date" required>
						</div>
						<div class="form-group">
							<label>Врач</label>
							<select name="doctor">
								<option>Терапевт</option>
								<option>Кардиолог</option>
								<option>Стоматолог</option>
							</select>
						</div>
						<button type="submit" class="btn">Добавить в базу</button>
					</form>
				</div>

				<div class="card">
					<h1>Журнал записей</h1>
					<table>
						<thead><tr><th>Пациент</th><th>Дата</th><th>Врач</th></tr></thead>
						<tbody>%s</tbody>
					</table>
				</div>
			</div>
		</body></html>`, sharedStyles, userInfo.Email, userInfo.Email, tableBody)
	})

	// Сохранение
	http.HandleFunc("/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			pName := r.FormValue("patient_name")
			pDate := r.FormValue("date")
			pDoc := r.FormValue("doctor")
			uEmail := r.FormValue("user_email")

			_, err := db.Exec("INSERT INTO appointments (patient_name, appointment_date, doctor_name, user_email) VALUES ($1, $2, $3, $4)",
				pName, pDate, pDoc, uEmail)

			if err != nil {
				http.Error(w, "DB Error", 500)
				return
			}
			// Возвращаем на главную после сохранения
			fmt.Fprintf(w, `<script>alert("Запись сохранена!"); window.location.href="/callback";</script>`)
		}
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
